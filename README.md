# modal-whisper

Speech-to-text transcription service using [WhisperX](https://github.com/m-bain/whisperX) (Whisper large-v3) deployed on [Modal](https://modal.com). Supports speaker diarization via pyannote.

## Setup

### 1. Modal account

Install the Modal CLI and authenticate:

```bash
pip install modal
modal token new
```

### 2. HuggingFace token (required for diarization)

Accept the pyannote model licenses on HuggingFace:
- https://huggingface.co/pyannote/segmentation-3.0
- https://huggingface.co/pyannote/speaker-diarization-3.1

Create a Modal secret with your HuggingFace token:

```bash
modal secret create huggingface-secret HF_TOKEN=hf_your_token_here
```

### 3. Deploy

```bash
modal deploy app.py
```

## Usage

The deployed function is called from the [plaud-cli](https://github.com/jaisonerick/plaud-cli) `transcribe` command:

```bash
export MODAL_TOKEN_ID=ak-...
export MODAL_TOKEN_SECRET=as-...
export MODAL_APP_NAME=modal-whisper
export MODAL_FUNCTION_NAME=WhisperTranscriber.transcribe

plaud transcribe <recording-id> --diarize
```

### Function contract

**Input:**
- `audio_data: bytes` — raw audio file (MP3, WAV, etc.)
- `diarize: bool` — enable speaker diarization (default: `False`)

**Output:** JSON array of segments:

```json
[
  {
    "start_time": 0,
    "end_time": 5200,
    "content": "Hello, welcome to the meeting.",
    "speaker": "SPEAKER_00"
  }
]
```

Times are in milliseconds. Speaker field is empty when `diarize=False`.

## Development

Run locally for testing:

```bash
modal run app.py
```

## Infrastructure

- **GPU:** NVIDIA A10G (24GB)
- **Model:** Whisper large-v3 (1.54B params)
- **Diarization:** pyannote-audio 3.1
- **Model cache:** Modal Volume (`whisper-model-cache`) to persist downloaded models across cold starts
