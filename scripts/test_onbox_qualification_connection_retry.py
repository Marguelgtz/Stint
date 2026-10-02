import pathlib
import subprocess
import tempfile
import unittest


class QualificationConnectionRetryTest(unittest.TestCase):
    def test_connection_failure_retries_but_failed_or_interrupted_work_does_not(self):
        source = (pathlib.Path(__file__).parent / 'launch-onbox-deep.sh').read_text()
        function = 'retry_connection_step() {' + source.split('retry_connection_step() {', 1)[1].split('\ncleanup_local()', 1)[0]
        for scenario, expected_calls, expected_code in [('connect', 2, 0), ('gate', 1, 1), ('interrupted', 1, 255)]:
            with self.subTest(scenario=scenario), tempfile.TemporaryDirectory() as directory:
                mock = pathlib.Path(directory) / 'ssh'
                count = pathlib.Path(directory) / 'count'
                mock.write_text('''#!/bin/sh
n=0; [ ! -f "$COUNT" ] || n=$(cat "$COUNT")
n=$((n+1)); echo "$n" > "$COUNT"
case "$SCENARIO" in
  connect) [ "$n" -gt 1 ] || { echo 'ssh: connect to host fixture port 22: Connection timed out' >&2; exit 255; } ;;
  gate) echo 'ADMISSION_CANARY_FAIL' >&2; exit 1 ;;
  interrupted) echo 'Connection to fixture closed by remote host.' >&2; exit 255 ;;
esac
''')
                mock.chmod(0o700)
                harness = function + '\nSSH=("$MOCK"); TRANSFER_ATTEMPTS=3; TRANSFER_RETRY_SECONDS=0\nretry_connection_step fixture true\n'
                import os
                env = {**os.environ, 'MOCK': str(mock), 'COUNT': str(count), 'SCENARIO': scenario}
                result = subprocess.run(['bash', '-c', harness], env=env, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
                self.assertEqual(result.returncode, expected_code)
                self.assertEqual(int(count.read_text()), expected_calls)


if __name__ == '__main__':
    unittest.main()
