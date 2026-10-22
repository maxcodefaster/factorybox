import json
import unittest
from unittest.mock import patch

import free_model_probe as probe


class FreeModelAdmission(unittest.TestCase):
    model = "example-free"
    catalog = {"data": [{"id": model}]}
    identity = "<tr><td>Example Free</td><td>example-free</td><td>" + probe.DOCUMENTED_COMPLETIONS + "</td><td>@ai-sdk/openai-compatible</td></tr>"
    pricing = "<tr><td>Example Free</td><td>Free</td><td>Free</td><td>Free</td><td>-</td></tr>"

    def test_requires_exact_catalog_identity_and_price(self):
        approved = probe.approve_free_model(self.model, self.catalog, self.identity + self.pricing)
        self.assertEqual(approved["pricing_row"], ["Example Free", "Free", "Free", "Free", "-"])

    def test_suffix_and_free_text_alone_do_not_authorize_inference(self):
        invalid = [self.identity, self.pricing, self.identity + self.pricing.replace("Free</td><td>-", "$0.01</td><td>-"),
                   self.identity.replace(probe.DOCUMENTED_COMPLETIONS, "https://other.invalid/v1") + self.pricing,
                   self.identity + self.pricing + self.pricing]
        for html in invalid:
            with self.subTest(html=html), self.assertRaises(ValueError):
                probe.approve_free_model(self.model, self.catalog, html)
        for catalog in ({"data": []}, {"data": [*self.catalog["data"], *self.catalog["data"]]}):
            with self.subTest(catalog=catalog), self.assertRaises(ValueError):
                probe.approve_free_model(self.model, catalog, self.identity + self.pricing)
        with self.assertRaises(ValueError):
            probe.approve_free_model("example", {"data": [{"id": "example"}]}, self.identity + self.pricing)

    def test_provider_url_redirect_and_method_are_bounded(self):
        with patch("urllib.request.build_opener") as opened:
            for url, payload in [("http://opencode.ai/zen/v1/models", None), (probe.COMPLETIONS, None), (probe.CATALOG, {})]:
                with self.subTest(url=url), self.assertRaises(ValueError):
                    probe.exchange(url, payload)
            opened.assert_not_called()
        with self.assertRaises(ValueError):
            probe.NoRedirect().redirect_request(None, None, 307, "redirect", {}, "https://other.invalid")


class ActualToolProtocol(unittest.TestCase):
    def reply(self, arguments=None):
        return {"choices": [{"finish_reason": "tool_calls", "message": {"role": "assistant", "content": None,
                 "tool_calls": [{"id": "owned-call", "type": "function", "function": {"name": "record_probe", "arguments": json.dumps(arguments or {"nonce": "nonce"})}}]}}]}

    def test_exact_tool_call_and_private_nonce(self):
        message, call_id = probe.tool_call(self.reply(), "nonce")
        self.assertEqual(call_id, "owned-call")
        self.assertEqual(message["role"], "assistant")
        with self.assertRaises(ValueError):
            probe.tool_call(self.reply({"nonce": "wrong"}), "nonce")
        with self.assertRaises(ValueError):
            probe.tool_call(self.reply({"nonce": "nonce", "unrequested": True}), "nonce")

    def test_multiple_or_incomplete_calls_are_not_success(self):
        reply = self.reply()
        reply["choices"][0]["message"]["tool_calls"] *= 2
        with self.assertRaises(ValueError):
            probe.tool_call(reply, "nonce")
        reply = self.reply()
        reply["choices"][0]["finish_reason"] = "length"
        with self.assertRaises(ValueError):
            probe.tool_call(reply, "nonce")


if __name__ == "__main__":
    unittest.main()
