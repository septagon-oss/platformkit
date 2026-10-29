"""The e2e runner exports the mail host it resolved, once, before it boots the application."""
import pathlib
import re

SCRIPT = pathlib.Path(__file__).with_name("e2e.sh").read_text()


def test_the_resolved_mail_host_is_exported_once_where_it_is_resolved():
    exports = re.findall(r'^export PLATFORMKIT_E2E_MAIL_HOST="\$mail_host"$', SCRIPT, re.M)
    assert len(exports) == 1
    assert SCRIPT.index(exports[0]) > SCRIPT.index("mail_host=")
