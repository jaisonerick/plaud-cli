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
    ) -> list[dict]:
        pipeline = self.model

        if context_doc:
            pipeline = pipeline.with_context(context_doc)
        if diarize:
            pipeline = pipeline.with_diarize()
        if polish:
            pipeline = pipeline.with_polish()
        if compact and diarize:
            pipeline = pipeline.with_compact(max_gap_ms=compact_gap)

        return pipeline.transcribe(audio_data, language=language)
