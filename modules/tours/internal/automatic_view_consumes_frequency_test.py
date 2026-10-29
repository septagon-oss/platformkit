import subprocess


def test_viewing_automatic_run_consumes_reservation():
    result = subprocess.run(
        [
            "go",
            "test",
            "./modules/tours/internal",
            "-run",
            "^TestViewingAutomaticRunConsumesReservation$",
            "-count=1",
        ],
        check=False,
        capture_output=True,
        text=True,
    )
    assert result.returncode == 0, result.stdout + result.stderr
