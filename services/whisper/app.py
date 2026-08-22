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

# The transcripts this service has made, so that none is made twice. They live
# apart from the voices because a voice is read on every naming and a
# transcript only when a recording comes back, and the two grow at different
# rates.
transcript_volume = modal.Volume.from_name("whisper-transcripts", create_if_missing=True)


def _person_json(person: dict) -> dict:
    """One shape for a person wherever this service returns one."""
    return {
        "name": f"{person['first_name']} {person['last_name']}",
        "first_name": person["first_name"],
        "last_name": person["last_name"],
        "company": person["company"],
        "created_by": person["created_by"],
    }


async def open_transcripts():
    """The transcript store, on the newest state of its volume."""
    from modal_whisper.transcript_store import TranscriptStore

    await transcript_volume.reload.aio()
    return TranscriptStore()


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
    volumes={"/cache": model_cache, "/speakers": speaker_volume, "/transcripts": transcript_volume},
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
                "client_id": os.environ["GOOGLE_DEVICE_CLIENT_ID"],
                "client_secret": os.environ["GOOGLE_DEVICE_CLIENT_SECRET"],
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
            from modal_whisper.transcript_store import with_labels, with_names

            audio_data = await audio.read()
            opts_json = json.loads(options)

            recording_id = opts_json.get("recording_id", "")
            if not recording_id:
                raise HTTPException(
                    status_code=400,
                    detail="recording_id is required: the voices of a transcription "
                           "are found again by the recording they came from",
                )

            # A transcript already made is handed back rather than made again.
            # Deciding otherwise costs minutes of GPU and, worse, separates the
            # voices afresh: the labels are renumbered, and every transcript
            # written from the run before points at voices that are gone.
            # A language settled by the caller is a statement that what is on
            # record came back in the wrong one, so it is not what to hand back.
            wanted_language = opts_json.get("language", "")

            if not opts_json.get("force"):
                transcripts = await open_transcripts()
                kept = transcripts.get(recording_id)
                if kept and wanted_language and wanted_language != kept.get("language", {}).get("code"):
                    kept = None
                if kept:
                    speakers = await _who_they_are_now(kept.get("voices", {}))
                    kept = {
                        **kept,
                        "segments": with_names(kept["segments"], speakers),
                        "speakers": speakers,
                        "reused": True,
                    }
                    if stream:
                        return StreamingResponse(
                            _one_event(kept), media_type="text/event-stream"
                        )
                    return JSONResponse(kept)

            opts = TranscribeOptions(
                language=opts_json.get("language", ""),
                context_doc=opts_json.get("context_doc", ""),
                recording_id=recording_id,
            )

            await speaker_volume.reload.aio()
            pipeline = TranscriptionPipeline(self.whisper_model, self.llm)

            def keep(result):
                """Store what was decoded, with the labels the run gave it."""
                if not result.get("segments"):
                    return
                from modal_whisper.transcript_store import TranscriptStore

                TranscriptStore().put(
                    result["audio_id"],
                    {**result, "segments": with_labels(result["segments"], result.get("speakers", {}))},
                )
                transcript_volume.commit()

            if stream:
                def generate():
                    try:
                        for event in pipeline.transcribe_stream(audio_data, opts):
                            if event.get("type") == "result":
                                keep(event)
                            yield f"data: {json.dumps(event)}\n\n"
                    except Exception as err:
                        # Without this the stream just stops, and the client is
                        # left to guess why from a truncated response.
                        yield f"data: {json.dumps(_error_event(err))}\n\n"
                    finally:
                        model_cache.commit()
                        # The embeddings this run wrote are how `speaker name`
                        # finds a voice afterwards, and an uncommitted write
                        # dies with the container that made it.
                        speaker_volume.commit()

                return StreamingResponse(
                    generate(), media_type="text/event-stream"
                )
            else:
                result = pipeline.transcribe(audio_data, opts)
                keep(result)
                await model_cache.commit.aio()
                await speaker_volume.commit.aio()
                return JSONResponse(result)

        @api.get("/speakers/{audio_id}")
        async def recording_voices(audio_id: str, who: Identity = Depends(caller)):
            """The voices this service holds for a recording.

            A transcript made here never reaches Plaud, whose own record goes
            on saying the recording has none. What answers whether it was
            transcribed is the voices it left behind.
            """
            store = await open_speaker_store()
            try:
                labels = sorted(store.get_audio_embeddings(audio_id))
            finally:
                store.close()
            return {"voices": labels}

        @api.post("/speakers/{audio_id}/whois")
        async def whois(
            audio_id: str,
            keys: str = Form(...),
            who: Identity = Depends(caller),
        ):
            """Who each voice of a recording is, for the keys a transcript kept.

            A key is the id given to a voice when the recording was separated,
            or the label a transcript written before those ids carries. Either
            way the answer comes from the embedding behind it, compared against
            the people known right now: a name is a thing of today, and the
            transcript only has to say which voice it meant.
            """
            from modal_whisper.speaker_match import DEFAULT_THRESHOLD, nearest

            wanted = json.loads(keys)

            store = await open_speaker_store()
            try:
                embeddings = store.voices_of(audio_id, wanted)
                known = store.all_voices()
            finally:
                store.close()

            voices = []
            for key in wanted:
                found = embeddings.get(key)
                if found is None:
                    voices.append(
                        {"key": key, "voice": "", "name": "", "distance": None, "known": False}
                    )
                    continue
                voice_id, embedding = found
                hit = nearest(embedding, known)
                name, distance = hit if hit else ("", None)
                if distance is None or distance >= DEFAULT_THRESHOLD:
                    name = ""
                voices.append(
                    {
                        "key": key,
                        "voice": voice_id,
                        "name": name,
                        "distance": distance,
                        "known": True,
                    }
                )

            return {"voices": voices}

        @api.get("/speakers")
        async def list_people():
            store = await open_speaker_store()
            people = store.people()
            store.close()
            return [_person_json(p) | {"voices": p["voices"]} for p in people]

        @api.patch("/speakers")
        async def rename_person(
            old: str = Form(...),
            new: str = Form(...),
            company: str = Form(...),
            surname_unknown: bool = Form(False),
            who: Identity = Depends(caller),
        ):
            """Correct who somebody is, or join two spellings of one person."""
            from modal_whisper.speaker_store import NotFull

            store = await open_speaker_store()
            try:
                person_id = store.person_id(old)
                if person_id is None:
                    raise HTTPException(status_code=404, detail=f"nobody is called {old!r}")
                store.rename_person(person_id, new, company, surname_unknown)
                person = store.person(person_id)
            except NotFull as err:
                raise HTTPException(status_code=400, detail=str(err)) from err
            finally:
                store.close()

            await speaker_volume.commit.aio()
            return {"person": _person_json(person)}

        @api.post("/speakers/forget")
        async def forget_person(name: str = Form(...)):
            """Drop a person and every voice of theirs."""
            store = await open_speaker_store()
            person_id = store.person_id(name)
            if person_id is None:
                store.close()
                raise HTTPException(status_code=404, detail=f"nobody is called {name!r}")
            store.forget_person(person_id)
            store.close()
            await speaker_volume.commit.aio()
            return {"name": name}

        @api.post("/speakers/enroll")
        async def enroll_speakers(
            audio: UploadFile = File(...),
            speakers: str = Form(...),
            who: Identity = Depends(caller),
        ):
            """Learn voices from a recording somebody already attributed.

            `speakers` is [{"name": str, "company": str, "ranges": [[ms, ms]]}].
            """
            from modal_whisper.speaker_store import NotFull
            from modal_whisper.transcribe import load_audio

            spec = json.loads(speakers)
            audio_array = load_audio(await audio.read())

            store = await open_speaker_store()
            enrolled, skipped = {}, {}
            try:
                for entry in spec:
                    name = entry["name"]
                    try:
                        person_id = store.upsert_person(
                            name,
                            entry.get("company", ""),
                            who.email,
                            entry.get("surname_unknown", False),
                        )
                    except NotFull as err:
                        skipped[name] = str(err)
                        continue
                    ranges = [(int(a), int(b)) for a, b in entry["ranges"]]
                    vector = self.embedder.embed(audio_array, ranges)
                    if vector is None:
                        skipped[name] = "too little speech to characterise a voice"
                        continue
                    store.add_voice(person_id, vector, who.email)
                    enrolled[name] = len(vector)
            finally:
                store.close()
            await speaker_volume.commit.aio()
            return {"enrolled": enrolled, "skipped": skipped}


        web_app.include_router(api)
        return web_app


async def _who_they_are_now(voices: dict) -> dict:
    """Who each label of a kept transcript is today.

    The transcript holds the id of the voice behind each label, and who that
    voice is comes from the people known right now: a name settled after a
    recording was transcribed has to reach the transcript of it.
    """
    from modal_whisper.speaker_match import DEFAULT_THRESHOLD, nearest

    if not voices:
        return {}

    store = await open_speaker_store()
    try:
        found = store.voices_of("", list(voices.values()))
        known = store.all_voices()
    finally:
        store.close()

    speakers = {}
    for label, voice_id in voices.items():
        speakers[label] = label
        held = found.get(voice_id)
        if held is None:
            continue
        hit = nearest(held[1], known)
        if hit and hit[1] < DEFAULT_THRESHOLD:
            speakers[label] = hit[0]
    return speakers


def _one_event(result: dict):
    """A transcript that was not made now still arrives as the stream a caller
    reads: one result, and nothing pretending to be work."""
    yield f"data: {json.dumps({'type': 'init', 'stages': []})}\n\n"
    yield f"data: {json.dumps({**result, 'type': 'result'})}\n\n"


def _error_event(err: Exception) -> dict:
    """Render an exception as the SSE event the client reports to the user."""
    message = str(err)
    if len(message) > 500:
        message = message[:500] + "..."
    return {"type": "error", "stage": "pipeline", "message": message}
