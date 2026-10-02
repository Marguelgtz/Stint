import pathlib
import subprocess
import tempfile
import unittest


class AdmissionContractTest(unittest.TestCase):
    def test_actual_task_and_final_verifiers_require_exact_marker_bytes(self):
        script = (pathlib.Path(__file__).parent / 'onbox-deep-admission-canary.sh').read_text()
        mission = script.split("<<'MISSION'\n", 1)[1].split('\nMISSION\n', 1)[0]
        task_command = next(line.strip().removeprefix('- verify: ') for line in mission.splitlines()
                            if line.strip().startswith('- verify: '))
        final_command = mission.split('## Verification\n\n', 1)[1].strip()
        self.assertEqual(task_command, final_command)
        cases = [(b'STINT_ADMISSION_OK\n', True), (b'STINT_ADMISSION_OK', False),
                 (b'STINT_ADMISSION_OK\n\n', False), (b'STINT_ADMISSION_OK\nextra\n', False),
                 (b'STINT_ADMISSION_OK\r\n', False)]
        with tempfile.TemporaryDirectory() as directory:
            marker = pathlib.Path(directory) / 'admission-canary.txt'
            for content, accepted in cases:
                with self.subTest(content=content):
                    marker.write_bytes(content)
                    result = subprocess.run(task_command, shell=True, cwd=directory,
                                            stdout=subprocess.PIPE, stderr=subprocess.PIPE)
                    self.assertEqual(result.returncode == 0, accepted)


if __name__ == '__main__':
    unittest.main()
