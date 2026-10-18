#!/usr/bin/env python3
"""Observe or finish the exact cleanup already requested by a failed lifecycle run."""
import argparse
import datetime
import json
import re
import sys
import uuid

import reference_lifecycle as runner


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("run", help="factorybox-lifecycle-<32 hex characters>")
    options = parser.parse_args()
    if not re.fullmatch(r"factorybox-lifecycle-[0-9a-f]{32}", options.run):
        parser.error("expected one exact lifecycle artifact name")
    archive = runner.ROOT / "artifacts" / options.run
    original = json.loads((archive / "result.json").read_text())
    if original["cleanup"]:
        print(json.dumps({"phase": "already_confirmed", "run": options.run}))
        return
    namespace, owner, uid = (original[key] for key in ["namespace", "owner", "namespace_uid"])
    observation = {"run": options.run, "namespace": namespace, "namespace_uid": uid,
                   "observed_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
                   "cleanup_confirmed": False, "production_qualified": False}
    try:
        text = runner.kube(["get", "namespace", namespace, "--ignore-not-found", "-o", "json"])
        if not text.strip():
            observation["cleanup_confirmed"] = True
            return
        ns = json.loads(text)
        runner.owned(ns["metadata"], namespace, owner, uid)
        observation["namespace"] = ns
        if not ns["metadata"].get("deletionTimestamp"):
            raise ValueError("namespace has no prior deletion request; do not adopt it")
        text = runner.kube(["get", "pod", "lifecycle", "-n", namespace,
                            "--ignore-not-found", "-o", "json"])
        if text.strip():
            pod = json.loads(text)
            runner.owned(pod["metadata"], "lifecycle", owner, original["pod_uid"])
            observation["pod"] = pod
            if pod.get("status", {}).get("phase") in ["Failed", "Succeeded"]:
                # A terminated pod can leave its API object behind when discovery
                # of an unrelated aggregated API is unhealthy. Remove only this
                # terminal object with its exact UID, preserving namespace finalizers.
                body = {"apiVersion": "v1", "kind": "DeleteOptions",
                        "preconditions": {"uid": original["pod_uid"]},
                        "gracePeriodSeconds": 0}
                runner.kube(["delete", "--raw",
                             "/api/v1/namespaces/" + namespace + "/pods/lifecycle", "-f", "-"],
                            input=json.dumps(body))
                observation["terminal_pod_delete_requested"] = True
        text = runner.kube(["get", "namespace", namespace, "--ignore-not-found", "-o", "json"])
        observation["cleanup_confirmed"] = not text.strip()
        if text.strip():
            runner.owned(json.loads(text)["metadata"], namespace, owner, uid)
    except Exception as error:
        observation["error"] = str(error)
        raise
    finally:
        path = archive / ("cleanup-observation-" + uuid.uuid4().hex + ".json")
        runner.write_json(path, observation)
        print(json.dumps({"evidence": str(path), "cleanup_confirmed":
                          observation["cleanup_confirmed"], "run": options.run}))
    if not observation["cleanup_confirmed"]:
        sys.exit(75)


if __name__ == "__main__":
    main()
