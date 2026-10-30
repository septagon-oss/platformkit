import subprocess


def test_lost_send_marks_stay_together():
    result = subprocess.run(
        [
            "go",
            "test",
            "./modules/auth/internal",
            "-count=1",
            "-run",
            "^TestAMarkThatFoundItsRowAlreadyUnknownSaysNothingAgain$",
        ],
        check=False,
        stdin=subprocess.DEVNULL,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
        text=True,
        timeout=300,
    )
    assert result.returncode == 0, result.stdout
