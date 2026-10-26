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
