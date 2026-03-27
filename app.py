import os

import modal

app = modal.App("modal-whisper")

image = (
    modal.Image.debian_slim(python_version="3.11")
    .apt_install("ffmpeg", "git")
    .pip_install(
        "whisperx @ git+https://github.com/m-bain/whisperX.git",
        "torch>=2.1",
        "torchaudio>=2.1",
        "litellm",
    )
    .add_local_python_source("modal_whisper")
    .add_local_dir("modal_whisper/prompts", remote_path="/prompts")
)

model_cache = modal.Volume.from_name("whisper-model-cache", create_if_missing=True)


@app.cls(
    gpu="A10G",
    image=image,
    secrets=[
        modal.Secret.from_name("huggingface-secret"),
        modal.Secret.from_name("openrouter-secret"),
    ],
    volumes={"/cache": model_cache},
    timeout=600,
    scaledown_window=120,
)
class WhisperTranscriber:
    @modal.enter()
    def setup(self):
        from modal_whisper import WhisperTranscriptionBuilder
        from modal_whisper.prompts import set_prompts_dir

        os.environ["HF_HOME"] = "/cache/huggingface"
        set_prompts_dir("/prompts")
        self.model = WhisperTranscriptionBuilder(
            device="cuda",
            compute_type="float16",
            model_name="large-v3",
            llm_model="openrouter/anthropic/claude-sonnet-4",
            llm_api_key=os.environ["OPENROUTER_API_KEY"],
        ).load()

    @modal.method()
    def transcribe(
        self,
        audio_data: bytes,
        diarize: bool = True,
        language: str = "",
        compact: bool = True,
        compact_gap: int = 2000,
        polish: bool = True,
        context_doc: str = "",
        speaker_recognition: bool = False,
        speaker_threshold: float = 0.35,
    ) -> dict:
        pipeline = self.model

        if context_doc:
            pipeline = pipeline.with_context(context_doc)
        if diarize:
            pipeline = pipeline.with_diarize()
        if speaker_recognition and diarize:
            pipeline = pipeline.with_speaker_recognition(threshold=speaker_threshold)
        if polish:
            pipeline = pipeline.with_polish()
        if compact and diarize:
            pipeline = pipeline.with_compact(max_gap_ms=compact_gap)

        result = pipeline.transcribe(audio_data, language=language)
        model_cache.commit()
        return result

    @modal.method()
    def set_speaker_name(self, audio_id: str, speaker_id: str, name: str) -> dict:
        from modal_whisper.speaker_store import SpeakerStore

        store = SpeakerStore()
        embedding = store.get_audio_speaker_info(audio_id, speaker_id)
        if embedding is None:
            store.close()
            return {"success": False, "error": f"No embedding found for {audio_id}/{speaker_id}"}

        store.set_known_speaker(name, embedding)
        store.close()
        model_cache.commit()
        return {"success": True, "name": name, "audio_id": audio_id, "speaker_id": speaker_id}

    @modal.method()
    def list_known_speakers(self) -> list[str]:
        from modal_whisper.speaker_store import SpeakerStore

        store = SpeakerStore()
        names = store.get_known_speaker_names()
        store.close()
        return names
