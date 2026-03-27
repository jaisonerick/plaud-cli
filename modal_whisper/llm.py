from concurrent.futures import ThreadPoolExecutor, as_completed

import litellm


class LLMClient:
    """LLM client with support for single and batch parallel calls."""

    def __init__(self, model: str, api_key: str, max_workers: int = 12):
        self.model = model
        self.api_key = api_key
        self.max_workers = max_workers

    def call(self, messages: list[dict]) -> str:
        """Make a single LLM call and return the response text."""
        response = litellm.completion(
            model=self.model,
            messages=messages,
            api_key=self.api_key,
            temperature=0,
        )
        return response.choices[0].message.content

    def call_batch(self, messages_list: list[list[dict]]) -> list[str]:
        """Make multiple LLM calls in parallel, preserving order."""
        results = [None] * len(messages_list)

        with ThreadPoolExecutor(max_workers=self.max_workers) as pool:
            futures = {
                pool.submit(self.call, msgs): i
                for i, msgs in enumerate(messages_list)
            }
            for future in as_completed(futures):
                idx = futures[future]
                results[idx] = future.result()

        return results


def strip_code_fences(text: str) -> str:
    """Remove markdown code fences from LLM output."""
    text = text.strip()
    if text.startswith("```"):
        text = text.split("\n", 1)[1] if "\n" in text else text[3:]
        if text.endswith("```"):
            text = text[:-3]
        text = text.strip()
    return text
