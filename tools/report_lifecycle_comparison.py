#!/usr/bin/env python3
"""Render verified comparison observations without estimating percentiles."""
import argparse
import json
from pathlib import Path
import re

import benchmark_lifecycle
import reference


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--comparison",required=True)
    args=parser.parse_args()
    if not re.fullmatch(r"factorybox-lifecycle-comparison-[0-9a-f]{32}",args.comparison):raise ValueError("requires one exact comparison")
    root=reference.ROOT/"artifacts"/args.comparison
    result=json.loads((root/"result.json").read_text())
    for row in result["rows"]:
        if row.get("passed") and benchmark_lifecycle.observations(reference.ROOT/"artifacts"/row["native_artifact"])!=row["observation"]:
            raise ValueError("native observations no longer match their comparison")
    report=benchmark_lifecycle.summarize(result["rows"])
    if report!=result["report"]:raise ValueError("stored medians/limits differ from the actual observations")
    lines=["# Factorybox lifecycle comparison", "",f"Source: `{args.comparison}/result.json`, SHA-256 `{reference.sha(root/'result.json')}`.","",
           "Five complete observations per configuration and example; all twenty native runs pass with exact cleanup. "
           "The original packaged 0.1 CLI/CRIU and candidate use the same delivered C9 Go worker, Sysbox services, frozen rootfs/project images and ceilings. "
           "Factory uses VFS in the reference and private Docker Overlay2 in the candidate; minimal remains VFS. "
           "Theia is a separate 1 CPU / 768 MiB peer. The whole sandbox worker has 3 CPUs / 3 GiB, at most two live sandboxes and a 420-second lifecycle deadline.","",
           "These are shared-desktop-host observations with uncontrolled host page cache. No cache clearing or tuning. "
           "Acquisition from the network and compilation are outside the native lifecycle; worker startup and complete local preparation are charged separately. "
           "The active-agent/tool cases are separate functional proofs; these measurements contain the unchanged ordinary consumers. No p95 or production latency guarantee.","",
           f"Factory halving target: **{report['factory_halving_target_met']}**. Declared regression limits: **{report['regression_limits_met']}**.",""]
    if result.get("interruptions"):
        lines += ["The storage guard stopped the sequence after fourteen successful runs, before another native run started. "
                  "Verified sharing of byte-identical closed test-program inputs freed space. The frozen source identity and all fourteen prior observations/result hashes "
                  "were reverified before continuing the remaining predetermined positions. Original values and order remain. "
                  "The gap and its possible cache effects are retained in `interruptions`, `before-resume-1.json` and the maintenance receipt; no measurement overlapped that maintenance.",""]
    for example,metrics in report["examples"].items():
        lines += [f"## {example}","","All durations are seconds. Cells show median [minimum, maximum]. Exact unrounded values remain in the source JSON.","",
                  "| Phase | Reference | Candidate | Reduction | Regression limit |","| --- | ---: | ---: | ---: | :---: |"]
        for name,values in metrics.items():
            def cell(configuration):
                value=values[configuration]
                return f"{value['median_seconds']:.3f} [{value['min_seconds']:.3f}, {value['max_seconds']:.3f}]"
            lines.append(f"| {name} | {cell('reference')} | {cell('candidate')} | {100*values['reduction_fraction']:.1f}% | {'pass' if values['within_regression_limit'] else 'FAIL'} |")
        lines += ["","### Every observation","","| Phase | Reference, in recorded order | Candidate, in recorded order |","| --- | --- | --- |"]
        for name,values in metrics.items():
            samples=lambda key:", ".join(f"{value:.6f}" for value in values[key]["values_seconds"])
            lines.append(f"| {name} | {samples('reference')} | {samples('candidate')} |")
        lines += ["","### Pause memory and checkpoint storage","",
                  "Bytes below are actual per-run counters, not a claim of zero whole-worker memory. "
                  "The source sandbox's anonymous RAM and resident RAM-image pages are zero after pause. "
                  "The worker retains services, test/control overhead and reclaimable filesystem page cache. "
                  "Docker data remains an independent complete mutable copy in each fork; Overlay2 deduplicates image representation within that private Docker state, not through a cross-fork global image store.","",
                  "| Configuration / run | Docker files / bytes | Source anon after pause | RAM-image bytes / resident after | Whole worker memory.current after pause | Whole worker anon / file after pause |",
                  "| --- | ---: | ---: | ---: | ---: | ---: |"]
        for row in result["rows"]:
            if row["example"]!=example or not row.get("passed"):continue
            observation=row["observation"]; memory=observation["pause_memory"]
            docker=next(tree for tree in observation["checkpoint_payloads"]["checkpoint-source"]["roots"] if tree["name"]=="volumes/docker")
            native=reference.ROOT/"artifacts"/row["native_artifact"]
            timings=[json.loads(path.read_text()) for path in (native/"prepare-output").glob("*.timing.json")]
            pause=next(value for value in timings if value["arguments"][0]=="pause")["worker_resources_after"]
            stat=dict(line.split() for line in pause["memory.stat"].splitlines())
            lines.append(f"| {row['configuration']} / {row['position']} | {docker['entries']} / {docker['file_bytes']} | {memory['after']['anon']} | {memory['snapshot']['bytes']} / {memory['snapshot']['resident_bytes_after']} | {pause['memory.current']} | {stat['anon']} / {stat['file']} |")
        lines += [""]
    lines += ["## Native runs and failures","","Every row retains frozen source, component/binary identities, original assertions, useful-work observations, worker/host CPU/I/O/memory pressure and command responses. "
              "No native run in this matrix failed or skipped. Setup/calibration failures and the older unexplained service timeout remain separately documented in `docs/pilot-progress.md`; they are not inserted into this predetermined ordinary-consumer timing matrix.","",
              "| Position | Configuration | Example | Evidence |","| ---: | --- | --- | --- |"]
    for row in result["rows"]:
        path=reference.ROOT/"artifacts"/row["native_artifact"]/"result.json"
        lines.append(f"| {row['position']} | {row['configuration']} | {row['example']} | [{row['native_artifact']}]({path}) |")
    lines += ["","Checkpoint timings are complete public CLI calls. Fork/useful-work timings start with a prepared checkpoint; "
              "`capture-plus-*` separately adds the corresponding fresh checkpoint. App and Theia first-use are separate observations. "
              "`cold-worker-to-useful` sums each run's worker startup, full local preparation and app first-use before taking the median; it is not the sum of independent phase medians.",""]
    target=root/"benchmark-report.md"
    with target.open("x") as stream:stream.write("\n".join(lines))
    print(json.dumps({"report":str(target),"sha256":reference.sha(target),"halving_target_met":report["factory_halving_target_met"],"regression_limits_met":report["regression_limits_met"]}))


if __name__=="__main__":main()
