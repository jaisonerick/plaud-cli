"""What of a decode is speech, and whether a recording held any.

A window the voice detector keeps by mistake comes back from the batched
decoder as a confident sentence rather than as nothing, because that path
never applies the fallback the sequential decoder does. Six seconds of a
microphone being put down came back as "Thank you." at -0.69, where speech
sits around -0.1.
"""

# What faster-whisper calls log_prob_threshold, and applies when it decodes a
# window on its own.
LOG_PROB_FLOOR = -1.0

# A recording holding less speech than this says too little to be a meeting,
# and is where the invented sentence lives.
NO_SPEECH_SECONDS = 15.0
SURE_ENOUGH = -0.5


def keep_speech(result: dict) -> tuple[dict, list[float]]:
    """Drop what the decoder itself would not have stood behind.

    The dropped scores come back so that a run can say what it refused: a
    stretch of a meeting that vanishes without a word reads as a meeting that
    was quieter than it was.
    """
    kept, dropped = [], []
    for seg in result.get("segments", []):
        score = seg.get("avg_logprob")
        if score is not None and score < LOG_PROB_FLOOR:
            dropped.append(round(float(score), 2))
            continue
        kept.append(seg)
    return {**result, "segments": kept}, dropped


def holds_speech(result: dict) -> bool:
    """Whether a recording said anything at all.

    Only a recording holding almost no speech is judged this way. A meeting
    has hours of it and passages the decoder finds hard, and those are hard
    rather than imagined.
    """
    segments = result.get("segments", [])
    if not segments:
        return False

    speech = sum(seg.get("end", 0) - seg.get("start", 0) for seg in segments)
    if speech >= NO_SPEECH_SECONDS:
        return True

    return any(
        seg.get("avg_logprob") is None or seg["avg_logprob"] >= SURE_ENOUGH
        for seg in segments
    )
