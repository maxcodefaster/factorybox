# Native detach fixture ownership

The Linux AMD64 release run rejected `/out/sysbox-runc` before executing the
timer test because the host-bound bundle file belonged to UID 1000. The delivered
image's executable belongs to root. Docker file-sharing ownership made this
test preparation depend on the host.

The owned fixture now copies the executable from the actual delivered image to
the original `/out/sysbox-runc` path and checks its SHA-256. It changes no host
file ownership. The production root-ownership check stays enabled. The original
native tests, 256 real detach completions and all three refusal controls stay
unchanged; vet remains enabled.

| Fixture file | Before SHA-256 | After SHA-256 |
| --- | --- | --- |
| `tools/bounded_store_native.py` | `cb5e5abeb9f346153b9d8fdf6d598d46cde331f7fbf08943ea95ddab358ea4d3` | `98df9813dff1b4f1e5a795557b0a5aec1d5ab1cac10fcd1bc7c3cfb5fe4f7e1a` |
| `tests/checkpoint-retirement-setup.sh` | `9174405371cdd3ec3399fdda4ec8fcd76ff077d96fc4ffa284faf03ddc075fec` | `aa63121a308e0e692b87151ae0b7e6d4a9d4d690fce59403e2a733303d47ce06` |
| `tools/test_bounded_store_native.py` | `1641db9fed968e6cfe3eba9b8fc0aeef5963c7da9bf288a3a51eb853f8316e89` | `d90f6f65cb4a89a63b25c17f78cd7b6dc74e205e6bacda8bc85b0017143238ca` |

The Python transport test now requires the image-based fixture instead of the
host bind. Its changed-input refusal remains. The original native Go assertion
files retain these exact hashes before and after:

* `internal/lifecycle/timer_detach_native_linux_test.go`:
  `7efc0c79cb8a7925d85ee479d8a88fcc2036e28c3401f806f4f3dff831fb6325`
* `internal/lifecycle/capture_guard_linux_test.go`:
  `d3b5584bd49fd88e0083512df009002f1d8339656d16af630133d3c425a2e911`

The original failing run is
`factorybox-checkpoint-retirement-8fc7c803a4ec463f8bc1a80bc3a1f1a6`.
The unchanged native regression passes with the corrected preparation in
`factorybox-checkpoint-retirement-a92ea814bea043f8be982be5aac678ca`.
Both outcomes and reconstruction receipts remain under `artifacts/`.

## Factory project image selection

The Factory native fixture requires its pinned Node project image. The release
workflow incorrectly selected the minimal Alpine example. It now selects the
Factory example. Both of that example's services use the same exact Node digest,
so the image archive correctly contains one unique image. Input preparation
still requires an image declaration for each of the two services.

| File | Before SHA-256 | After SHA-256 |
| --- | --- | --- |
| `tools/devx_project_images.py` | `fdd5c2599fbb22f0e00b17dfe52570685e23138c7d774887e79750b503eaeaba` | `18b42dd4372f6edc783622755a92177dc591f9a8743bb61bd9d9a62f5e569d98` |
| `.github/workflows/images.yaml` | `fad0327d825798b7224b10e7844ef0e3fed9fd2f42a29ddc478a6014ab03c514` | `4d5656c6d627fa44a76aae76039cc0cb37eb3da7afbd5466bbc3c5cdad729d6b` |
| `tools/test_minimal_images.py` | `3edb4ef327ad21b4ee330a5ea250afa8b33b321fc3822f09c6e1c41789655a98` | `cd267ecf64fcb949a3c52b05242486a88ef18ad224ea9c8ae9846a95203ef426` |

The consumer's assertions in `tools/factory_consumer_native.py` remain unchanged
at `1892bfa8afaf40124f7236d1c19980e8e505593cc39b07e0f00d6c7df33632b3`.
All existing project-image tests remain, with an added shared-image regression.
