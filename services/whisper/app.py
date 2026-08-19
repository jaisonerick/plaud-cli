import json
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
        "fastapi[standard]",
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
        """Load expensive resources once — never mutated after this."""
        from modal_whisper.model import WhisperModel
        from modal_whisper.llm import LLMClient
        from modal_whisper.prompts import set_prompts_dir

        os.environ["HF_HOME"] = "/cache/huggingface"
        set_prompts_dir("/prompts")

        self.whisper_model = WhisperModel(
            device="cuda",
            compute_type="float16",
            model_name="large-v3",
        )
        self.whisper_model.load()

        self.llm = LLMClient(
            model="openrouter/anthropic/claude-sonnet-4",
            api_key=os.environ["OPENROUTER_API_KEY"],
        )

    @modal.asgi_app(label="modal-whisper", requires_proxy_auth=True)
    def web(self):
        from fastapi import FastAPI, File, Form, HTTPException, UploadFile
        from fastapi.responses import JSONResponse, StreamingResponse

        from modal_whisper.builder import TranscribeOptions, TranscriptionPipeline

        web_app = FastAPI()

        @web_app.post("/transcribe")
        async def transcribe(
            audio: UploadFile = File(...),
            options: str = Form("{}"),
            stream: bool = False,
        ):
            audio_data = await audio.read()
            opts_json = json.loads(options)

            opts = TranscribeOptions(
                language=opts_json.get("language", ""),
                context_doc=opts_json.get("context_doc", ""),
                diarize=opts_json.get("diarize", True),
                speaker_recognition=opts_json.get("speaker_recognition", False),
                speaker_threshold=opts_json.get("speaker_threshold", 0.35),
                polish=opts_json.get("polish", True),
                compact=opts_json.get("compact", True) and opts_json.get("diarize", True),
                compact_gap=opts_json.get("compact_gap", 2000),
            )

            pipeline = TranscriptionPipeline(self.whisper_model, self.llm)

            if stream:
                def generate():
                    try:
                        for event in pipeline.transcribe_stream(audio_data, opts):
                            yield f"data: {json.dumps(event)}\n\n"
                    finally:
                        model_cache.commit()

                return StreamingResponse(
                    generate(), media_type="text/event-stream"
                )
            else:
                result = pipeline.transcribe(audio_data, opts)
                model_cache.commit()
                return JSONResponse(result)

        @web_app.put("/speakers/{audio_id}/{speaker_id}")
        async def set_speaker_name(
            audio_id: str, speaker_id: str, name: str = Form(...)
        ):
            from modal_whisper.speaker_store import SpeakerStore

            store = SpeakerStore()
            embedding = store.get_audio_speaker_info(audio_id, speaker_id)
            if embedding is None:
                store.close()
                raise HTTPException(
                    status_code=404,
                    detail=f"No embedding found for {audio_id}/{speaker_id}",
                )

            store.set_known_speaker(name, embedding)
            store.close()
            model_cache.commit()
            return {
                "success": True,
                "name": name,
                "audio_id": audio_id,
                "speaker_id": speaker_id,
            }

        @web_app.get("/speakers")
        async def list_known_speakers():
            from modal_whisper.speaker_store import SpeakerStore

            store = SpeakerStore()
            names = store.get_known_speaker_names()
            store.close()
            return names

        return web_app
