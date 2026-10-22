import json
import unittest

import model_relay


class SyntheticInferenceAdmission(unittest.TestCase):
    def test_free_identity_and_actual_client_are_both_required(self):
        headers = {"authorization": "Bearer public", "user-agent": "opencode/2.0.27", "x-opencode-session": "synthetic", "x-opencode-client": "opencode"}
        body = json.dumps({"model": "test-free", "messages": [{"role": "user", "content": "synthetic"}]}).encode()
        self.assertEqual(model_relay.admit("/v1/chat/completions", headers, body, "test-free")["model"], "test-free")
        for changes in [{"authorization": "Bearer private"}, {"user-agent": "other"}, {"x-opencode-session": ""}]:
            with self.subTest(changes=changes), self.assertRaises(ValueError):
                model_relay.admit("/v1/chat/completions", {**headers, **changes}, body, "test-free")
        for path, model in [("https://other.invalid", "test-free"), ("/v1/chat/completions", "paid")]:
            with self.subTest(path=path, model=model), self.assertRaises(ValueError):
                model_relay.admit(path, headers, body, model)


if __name__ == "__main__":
    unittest.main()
