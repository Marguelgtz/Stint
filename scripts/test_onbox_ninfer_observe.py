#!/usr/bin/env python3
"""Fixtures for the bounded NInfer runtime evidence sampler."""
import importlib.util
import json
import pathlib
import sys
import tempfile
import unittest
from unittest import mock


MODULE = pathlib.Path(__file__).with_name("onbox-ninfer-observe.py")
SPEC = importlib.util.spec_from_file_location("onbox_ninfer_observe", MODULE)
OBSERVE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(OBSERVE)


class NInferObserveTests(unittest.TestCase):
    def test_metrics_allowlist_slots_and_derived_rates(self):
        metrics = OBSERVE.parse_metrics("""
# HELP llamacpp:prompt_tokens_total prompts
llamacpp:prompt_tokens_total 100
llamacpp:tokens_predicted_total 50
llamacpp:requests_processing 2
ninfer:prefix_cache_hit_tokens_total 20
unsafe_secret_metric{token="do-not-save"} 9
""")
        self.assertEqual(metrics["llamacpp:prompt_tokens_total"], 100)
        self.assertNotIn("unsafe_secret_metric", metrics)
        slots = OBSERVE.safe_slots('[{"id":0,"is_processing":true,"retained":true,"n_ctx":8192,"n_prompt_tokens":100,"session_digest":"secret","prompt":"private"}]')
        self.assertEqual(slots[0]["id"], 0)
        self.assertNotIn("session_digest", slots[0])
        self.assertNotIn("prompt", slots[0])
        rates = OBSERVE.derive_rates(
            {"llamacpp:prompt_tokens_total": 50, "ninfer:prefix_cache_hit_tokens_total": 10, "llamacpp:tokens_predicted_total": 20},
            {"llamacpp:prompt_tokens_total": 150, "ninfer:prefix_cache_hit_tokens_total": 30, "llamacpp:tokens_predicted_total": 80},
            10,
        )
        self.assertEqual(rates["prefillNonCachedTokensPerSecond"], 10)
        self.assertEqual(rates["prefixCacheHitTokensPerSecond"], 2)
        self.assertEqual(rates["prefillTokensPerSecond"], 12)
        self.assertEqual(rates["liveEngineDecodeTokensPerSecond"], 6)
        self.assertIn("separate from benchmark", rates["decodeRateBasis"])

    def test_sample_records_configured_clients_separate_from_slot_rows(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            state_dir = root / "deep" / "session-1"
            state_dir.mkdir(parents=True)
            latest = root / "deep" / "latest"
            latest.write_text("session-1\n", encoding="utf-8")
            session = root / "session.json"
            session.write_text(json.dumps({"clients": 2}), encoding="utf-8")
            responses = [
                "llamacpp:requests_processing 1\nllamacpp:requests_deferred 0\nllamacpp:prompt_tokens_total 45\n",
                '[{"id":0,"is_processing":true,"retained":true,"n_ctx":8192},{"id":1,"is_processing":false,"retained":true,"n_ctx":8192},{"id":2,"is_processing":false},{"id":3,"is_processing":false}]',
            ]
            with mock.patch.object(OBSERVE, "fetch", side_effect=responses):
                counters, _ = OBSERVE.sample(str(latest), str(session), {}, None)
            self.assertEqual(counters["llamacpp:requests_processing"], 1)
            record = json.loads((state_dir / "ninfer-runtime.jsonl").read_text(encoding="utf-8"))
            self.assertEqual(record["configuredClients"], 2)
            self.assertEqual(record["exposedEngineSlotRows"], 4)
            self.assertEqual(record["requestsDeferred"], 0)
            self.assertNotIn("caller", json.dumps(record).lower())

    def test_retention_is_bounded_and_private(self):
        with tempfile.TemporaryDirectory() as directory:
            path = pathlib.Path(directory, "samples.jsonl")
            old_count = OBSERVE.MAX_SAMPLES
            old_bytes = OBSERVE.MAX_BYTES
            OBSERVE.MAX_SAMPLES = 2
            OBSERVE.MAX_BYTES = 1024
            try:
                OBSERVE.append_bounded(path, {"sample": 1})
                OBSERVE.append_bounded(path, {"sample": 2})
                OBSERVE.append_bounded(path, {"sample": 3})
                records = path.read_text(encoding="utf-8").splitlines()
                self.assertEqual([json.loads(item)["sample"] for item in records], [2, 3])
                self.assertEqual(path.stat().st_mode & 0o777, 0o600)
            finally:
                OBSERVE.MAX_SAMPLES = old_count
                OBSERVE.MAX_BYTES = old_bytes

    def test_retention_obeys_combined_byte_limit(self):
        with tempfile.TemporaryDirectory() as directory:
            path = pathlib.Path(directory, "samples.jsonl")
            row_bytes = len((json.dumps({"sample": 1}, separators=(",", ":")) + "\n").encode("utf-8"))
            max_bytes = (row_bytes * 2) - 1
            for sample in (1, 2, 3):
                OBSERVE.append_bounded(path, {"sample": sample}, max_samples=10, max_bytes=max_bytes)
            records = path.read_text(encoding="utf-8").splitlines()
            self.assertEqual([json.loads(item)["sample"] for item in records], [3])
            self.assertLessEqual(path.stat().st_size, max_bytes)

    def test_retention_trims_existing_small_rows_on_resume(self):
        with tempfile.TemporaryDirectory() as directory:
            path = pathlib.Path(directory, "samples.jsonl")
            path.write_text('{"sample":1}\n{"sample":2}\n{"sample":3}\n', encoding="utf-8")
            old_count = OBSERVE.MAX_SAMPLES
            old_bytes = OBSERVE.MAX_BYTES
            OBSERVE.MAX_SAMPLES = 2
            OBSERVE.MAX_BYTES = 1024
            try:
                OBSERVE.append_bounded(path, {"sample": 4})
                records = path.read_text(encoding="utf-8").splitlines()
                self.assertEqual([json.loads(item)["sample"] for item in records], [3, 4])
            finally:
                OBSERVE.MAX_SAMPLES = old_count
                OBSERVE.MAX_BYTES = old_bytes

    def test_sampler_runs_past_retention_window_until_stopped(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            latest = root / "deep" / "latest"
            state_dir = root / "deep" / "session-1"
            state_dir.mkdir(parents=True)
            latest.write_text("session-1\n", encoding="utf-8")
            session = root / "session.json"
            session.write_text('{"clients":2}\n', encoding="utf-8")
            calls = []

            def fake_sample(_latest, _session, _previous, _previous_mono, max_samples):
                calls.append(max_samples)
                return {}, float(len(calls))

            def stop_after_three(_seconds):
                if len(calls) >= 3:
                    raise KeyboardInterrupt

            with mock.patch.object(sys, "argv", ["observer", "--latest", str(latest), "--session", str(session), "--max-samples", "2"]), \
                 mock.patch.object(OBSERVE, "sample", side_effect=fake_sample), \
                 mock.patch.object(OBSERVE.time, "sleep", side_effect=stop_after_three):
                with self.assertRaises(KeyboardInterrupt):
                    OBSERVE.main()
            self.assertEqual(calls, [2, 2, 2], "max-samples should bound retention, not observer lifetime")


if __name__ == "__main__":
    unittest.main()
