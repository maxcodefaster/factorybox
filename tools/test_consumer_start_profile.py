import json
import unittest

import consumer_start_profile as profile


class RecordedStartupProfile(unittest.TestCase):
    def test_requires_whole_connector_and_does_not_sum_nested_spans(self):
        names = ["private-docker", "checkout", "devcontainer-configuration", "project-images", "devcontainer-merged-configuration",
                 "devcontainer-start", "tool-injection", "process-start", "channel-readiness"]
        connector = "\n".join("factory-startup " + json.dumps({"phase": name, "durationMs": 100}) for name in names)
        logs = [dict(type="start", text="Resolving Remote", timestamp=1000),
                dict(type="text", text="Failed to fetch control manifest: EAI_AGAIN", timestamp=6030),
                dict(type="stop", text="nested work", timestamp=7000, startTimestamp=6100),
                dict(type="stop", text="Resolving Remote", timestamp=7100, startTimestamp=1000)]
        parsed = profile.parse(connector, "\n".join(map(json.dumps, logs)))
        self.assertEqual(parsed["connector_phase_milliseconds"], 900)
        self.assertEqual(parsed["control_manifest_failures"][0]["milliseconds_since_resolving_remote"], 5030)
        self.assertEqual([row["milliseconds"] for row in parsed["devcontainer_slow_spans"]], [900, 6100])
        with self.assertRaises(ValueError):
            profile.parse(connector + "\n" + connector, "")
        with self.assertRaises(ValueError):
            profile.parse("", "")


if __name__ == "__main__":
    unittest.main()
