import math
import os
import tempfile

import modal

app = modal.App("modal-whisper")

image = (
    modal.Image.debian_slim(python_version="3.11")
    .apt_install("ffmpeg", "git")
    .pip_install(
        "whisperx @ git+https://github.com/m-bain/whisperX.git",
        "torch>=2.1",
        "torchaudio>=2.1",
    )
)

model_cache = modal.Volume.from_name("whisper-model-cache", create_if_missing=True)


@app.cls(
    gpu="A10G",
    image=image,
    secrets=[modal.Secret.from_name("huggingface-secret")],
    volumes={"/cache": model_cache},
    timeout=600,
    scaledown_window=120,
)
class WhisperTranscriber:
    @modal.enter()
    def load_models(self):
        import whisperx

        self.device = "cuda"
        self.compute_type = "float16"

        os.environ["HF_HOME"] = "/cache/huggingface"

        self.model = whisperx.load_model(
            "large-v3",
            self.device,
            compute_type=self.compute_type,
        )

    @modal.method()
    def transcribe(self, audio_data: bytes, diarize: bool = False) -> list[dict]:
        import torch
        import whisperx

        hf_token = os.environ.get("HF_TOKEN", "")

        # Write audio bytes to a temp file for whisperx
        with tempfile.NamedTemporaryFile(suffix=".mp3", delete=False) as f:
            f.write(audio_data)
            audio_path = f.name

        try:
            # 1. Transcribe
            audio = whisperx.load_audio(audio_path)
            result = self.model.transcribe(audio, batch_size=16)
            language = result.get("language", "en")

            # 2. Align for word-level timestamps
            align_model, align_metadata = whisperx.load_align_model(
                language_code=language,
                device=self.device,
            )
            result = whisperx.align(
                result["segments"],
                align_model,
                align_metadata,
                audio,
                self.device,
                return_char_alignments=False,
            )

            # 3. Optionally diarize
            if diarize and hf_token:
                from pyannote.audio import Pipeline as PyannotePipeline

                diarize_model = PyannotePipeline.from_pretrained(
                    "pyannote/speaker-diarization-community-1",
                    token=hf_token,
                    cache_dir="/cache/huggingface",
                ).to(torch.device("cuda"))

                diarize_result = diarize_model(audio_path)

                # Assign speakers to segments based on overlap with diarization
                for seg in result["segments"]:
                    seg_start = seg["start"]
                    seg_end = seg["end"]
                    best_speaker = ""
                    best_overlap = 0.0

                    for turn, _, speaker in diarize_result.itertracks(yield_label=True):
                        overlap = max(0, min(seg_end, turn.end) - max(seg_start, turn.start))
                        if overlap > best_overlap:
                            best_overlap = overlap
                            best_speaker = speaker

                    seg["speaker"] = best_speaker

            # 4. Convert to plaud-cli contract format
            segments = []
            for seg in result["segments"]:
                start_ms = int(math.floor(seg.get("start", 0) * 1000))
                end_ms = int(math.floor(seg.get("end", 0) * 1000))
                text = seg.get("text", "").strip()
                speaker = seg.get("speaker", "") if diarize else ""

                if text:
                    segments.append({
                        "start_time": start_ms,
                        "end_time": end_ms,
                        "content": text,
                        "speaker": speaker,
                    })

            return segments
        finally:
            os.unlink(audio_path)
