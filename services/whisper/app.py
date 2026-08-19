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
        "google-auth",
    )
    .add_local_python_source("modal_whisper")
    .add_local_dir("modal_whisper/prompts", remote_path="/prompts")
)

model_cache = modal.Volume.from_name("whisper-model-cache", create_if_missing=True)

# The voices live apart from the model cache because only one of the two can be
# reloaded: HuggingFace keeps a log file open under the model cache, and one
# open file anywhere on a volume blocks reloading all of it.
speaker_volume = modal.Volume.from_name("whisper-speakers", create_if_missing=True)


async def open_speaker_store():
    """Open the speaker database on the newest state of the volume.

    Each container keeps its own view of a volume, so a write from another one
    stays invisible until reloaded — and committing on top of a view that never
    saw it publishes the file without it, silently undoing the write. Enrolling
    a library hits this on every recording after the first.
    """
    from modal_whisper.speaker_store import SpeakerStore

    await speaker_volume.reload.aio()
    return SpeakerStore()


@app.cls(
    gpu="A10G",
    image=image,
    secrets=[
        modal.Secret.from_name("huggingface-secret"),
        modal.Secret.from_name("openrouter-secret"),
        modal.Secret.from_name("google-oauth"),
    ],
    volumes={"/cache": model_cache, "/speakers": speaker_volume},
    timeout=600,
    scaledown_window=120,
)
class WhisperTranscriber:
    @modal.enter()
    def setup(self):
        """Load expensive resources once — never mutated after this."""
        from modal_whisper.model import WhisperModel
        from modal_whisper.embed import SpeakerEmbedder
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

        self.embedder = SpeakerEmbedder(
            device="cuda", hf_token=os.environ.get("HF_TOKEN", "")
        )

        self.llm = LLMClient(
            model="openrouter/anthropic/claude-sonnet-5",
            api_key=os.environ["OPENROUTER_API_KEY"],
        )

    @modal.asgi_app(label="modal-whisper")
    def web(self):
        from fastapi import (
            APIRouter,
            Depends,
            FastAPI,
            File,
            Form,
            Header,
            HTTPException,
            UploadFile,
        )
        from fastapi.responses import JSONResponse, StreamingResponse

        from modal_whisper.auth import (
            ALLOWED_DOMAINS,
            Identity,
            Unauthorized,
            identify,
        )
        from modal_whisper.builder import TranscribeOptions, TranscriptionPipeline

        web_app = FastAPI()

        async def caller(authorization: str | None = Header(default=None)) -> Identity:
            try:
                return identify(authorization)
            except Unauthorized as err:
                raise HTTPException(status_code=401, detail=str(err)) from err

        @web_app.get("/auth/config")
        async def auth_config():
            """The sign-in details, which a caller needs before it has a token.

            Deliberately the one route outside the guest list. It carries the
            OAuth client of an installed application, which Google does not
            treat as a secret, and nothing about who may use this service.
            """
            return {
                "client_id": os.environ["GOOGLE_CLIENT_ID"],
                "client_secret": os.environ["GOOGLE_CLIENT_SECRET"],
                "auth_uri": "https://accounts.google.com/o/oauth2/v2/auth",
                "token_uri": "https://oauth2.googleapis.com/token",
                "scopes": ["openid", "email", "profile"],
                "domains": list(ALLOWED_DOMAINS),
            }

        # Everything else hangs off this router, so a route added later cannot
        # forget to ask who is calling.
        api = APIRouter(dependencies=[Depends(caller)])

        @api.post("/transcribe")
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
                recording_id=opts_json.get("recording_id", ""),
                diarize=opts_json.get("diarize", True),
                speaker_recognition=opts_json.get("speaker_recognition", False),
                speaker_threshold=opts_json.get("speaker_threshold", 0.35),
                polish=opts_json.get("polish", True),
                compact=opts_json.get("compact", True) and opts_json.get("diarize", True),
                compact_gap=opts_json.get("compact_gap", 2000),
            )

            await speaker_volume.reload.aio()
            pipeline = TranscriptionPipeline(self.whisper_model, self.llm)

            if stream:
                def generate():
                    try:
                        for event in pipeline.transcribe_stream(audio_data, opts):
                            yield f"data: {json.dumps(event)}\n\n"
                    except Exception as err:
                        # Without this the stream just stops, and the client is
                        # left to guess why from a truncated response.
                        yield f"data: {json.dumps(_error_event(err))}\n\n"
                    finally:
                        model_cache.commit()

                return StreamingResponse(
                    generate(), media_type="text/event-stream"
                )
            else:
                result = pipeline.transcribe(audio_data, opts)
                await model_cache.commit.aio()
                return JSONResponse(result)

        @api.put("/speakers/{audio_id}/{speaker_id}")
        async def set_speaker_name(
            audio_id: str, speaker_id: str, name: str = Form(...)
        ):
            store = await open_speaker_store()
            embedding = store.get_audio_speaker_info(audio_id, speaker_id)
            if embedding is None:
                known = sorted(store.get_audio_embeddings(audio_id))
                store.close()
                if known:
                    detail = (
                        f"{audio_id} has no speaker {speaker_id!r}. "
                        f"It has: {', '.join(known)}"
                    )
                else:
                    detail = (
                        f"nothing is stored for recording {audio_id} — "
                        "transcribe it with diarization first"
                    )
                raise HTTPException(status_code=404, detail=detail)

            store.set_known_speaker(name, embedding)
            store.close()
            await speaker_volume.commit.aio()
            return {
                "success": True,
                "name": name,
                "audio_id": audio_id,
                "speaker_id": speaker_id,
            }

        @api.get("/speakers")
        async def list_known_speakers():
            store = await open_speaker_store()
            counts = store.get_known_speaker_counts()
            store.close()
            return [{"name": name, "samples": n} for name, n in counts]

        @api.post("/speakers")
        async def add_known_speaker(name: str = Form(...), embedding: str = Form(...)):
            """Register a voice from an embedding the caller already holds.

            This is what lets a speaker be named from a saved transcript: the
            embedding rode along in the file, so there is nothing here to look
            up and no audio to upload again.
            """
            vector = json.loads(embedding)
            if not isinstance(vector, list) or not vector:
                raise HTTPException(
                    status_code=400, detail="embedding must be a non-empty array"
                )

            store = await open_speaker_store()
            store.set_known_speaker(name, vector)
            samples = dict(store.get_known_speaker_counts()).get(name, 1)
            store.close()
            await speaker_volume.commit.aio()
            return {"name": name, "samples": samples}

        @api.patch("/speakers")
        async def rename_known_speaker(old: str = Form(...), new: str = Form(...)):
            """Move every sample of one spelling onto another.

            Samples split across two spellings of one person can only be
            rejoined by someone who knows they are the same person.
            """
            store = await open_speaker_store()
            moved = store.rename_known_speaker(old, new)
            store.close()
            if moved == 0:
                raise HTTPException(status_code=404, detail=f"no speaker named {old!r}")
            await speaker_volume.commit.aio()
            return {"old": old, "new": new, "moved": moved}

        # Spelled as a POST because the platform in front of this app answers a
        # DELETE with 405 before it ever arrives, however the route is declared.
        @api.post("/speakers/forget")
        async def forget_known_speaker(name: str = Form(...)):
            """Drop a voice, for when a wrong one was learned.

            A sample attributed to the wrong person does not sit there inertly:
            it is matched against every transcription from then on.
            """
            store = await open_speaker_store()
            dropped = store.forget_known_speaker(name)
            store.close()
            if dropped == 0:
                raise HTTPException(status_code=404, detail=f"no speaker named {name!r}")
            await speaker_volume.commit.aio()
            return {"name": name, "dropped": dropped}

        @api.post("/speakers/enroll")
        async def enroll_speakers(
            audio: UploadFile = File(...), speakers: str = Form(...)
        ):
            """Register voices from a recording somebody already attributed.

            `speakers` is [{"name": str, "ranges": [[start_ms, end_ms], ...]}].
            """
            from modal_whisper.transcribe import load_audio

            spec = json.loads(speakers)
            audio_array = load_audio(await audio.read())

            store = await open_speaker_store()
            enrolled, skipped = {}, {}
            try:
                for entry in spec:
                    name = entry["name"]
                    ranges = [(int(a), int(b)) for a, b in entry["ranges"]]
                    vector = self.embedder.embed(audio_array, ranges)
                    if vector is None:
                        skipped[name] = "too little speech to characterise a voice"
                        continue
                    store.set_known_speaker(name, vector)
                    enrolled[name] = len(vector)
            finally:
                store.close()
            await speaker_volume.commit.aio()
            return {"enrolled": enrolled, "skipped": skipped}

        web_app.include_router(api)
        return web_app


def _error_event(err: Exception) -> dict:
    """Render an exception as the SSE event the client reports to the user."""
    message = str(err)
    if len(message) > 500:
        message = message[:500] + "..."
    return {"type": "error", "stage": "pipeline", "message": message}
