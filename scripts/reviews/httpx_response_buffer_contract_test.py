import subprocess


def test_http_refusal_is_one_complete_document():
    result = subprocess.run(
        [
            "go",
            "test",
            "./kit/httpx",
            "-run",
            "^TestEveryGuardRefusalInTheChainIsOneDocument$",
            "-count=1",
        ],
        capture_output=True,
        text=True,
        timeout=600,
        check=False,
    )
    assert result.returncode == 0, result.stdout + result.stderr
