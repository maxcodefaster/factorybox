#!/usr/bin/env python3
"""Extend the frozen backing-retirement gate with previous-boot metadata coverage."""
import bounded_store_native


def main():
    # Keep the original runner's bytes, ownership checks, proof fields and all
    # required tests. Its coverage verifier must also see this added regression.
    bounded_store_native.BACKING_RETIREMENT_UNIT_TESTS = [
        *bounded_store_native.BACKING_RETIREMENT_UNIT_TESTS,
        "TestNativeRetainedMetadataAcrossPreviousBoot",
    ]
    bounded_store_native.main()


if __name__ == "__main__":
    main()
