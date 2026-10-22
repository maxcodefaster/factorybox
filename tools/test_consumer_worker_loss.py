import json
import subprocess
import time
import unittest
from unittest.mock import patch

import consumer_worker_loss


class ReadinessUsesDeliveredSupervisor(unittest.TestCase):
    @patch("consumer_worker_loss.time.sleep")
    @patch("consumer_worker_loss.subprocess.run")
    @patch("consumer_worker_loss.worker_readiness.published", side_effect=[False, True])
    @patch("consumer_worker_loss.exact_worker", return_value={"State": {"Running": True}})
    def test_baseline_cli_is_not_used_as_a_new_worker_status_client(self, exact, published, run, sleep):
        run.return_value = subprocess.CompletedProcess([], 0, json.dumps({"schema": 1, "store_id": "owned"}), "")
        consumer_worker_loss.ready("name", "owned", "identity", time.monotonic()+5)
        self.assertEqual(run.call_count, 1)
        self.assertEqual(run.call_args.args[0][-3:], ["identity", "/opt/factorybox/factorybox", "worker-status"])
        self.assertEqual(published.call_count, 2)
