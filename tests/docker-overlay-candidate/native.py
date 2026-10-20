"""Unselected one-variable overlay2 consumer experiment with all native checks."""
from pathlib import Path
import sys

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "tools"))
import devx_lifecycle
import reference_lifecycle

copy_original = devx_lifecycle.shutil.copyfile
write_original = reference_lifecycle.write_json


def copy_overlay_input(source, destination, *args, **kwargs):
    result = copy_original(source, destination, *args, **kwargs)
    source, destination = Path(source), Path(destination)
    if source == ROOT / "tests/devx-sandbox.sh":
        text = destination.read_text()
        if text.count("--storage-driver=vfs") != 1:
            raise ValueError("original storage driver does not match the controlled experiment")
        destination.write_text(text.replace("--storage-driver=vfs", "--storage-driver=overlay2"))
    if source == ROOT / "tools/devx_lifecycle.py":
        copy_original(__file__, destination.parent / "overlay2-experiment.py")
    return result


def write_overlay_result(path, result):
    if isinstance(result, dict) and result.get("suite") in {"lifecycle", "docker-interruption"}:
        result["scope"] = result["scope"].replace("Docker vfs", "Docker overlay2")
        result["storage_driver_experiment"] = "overlay2"
        result["diagnostic_only"] = True
        result["driver_selected"] = False
    return write_original(path, result)


devx_lifecycle.shutil.copyfile = copy_overlay_input
reference_lifecycle.write_json = write_overlay_result
devx_lifecycle.main()
