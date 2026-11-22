import importlib.util
import io
import hashlib
import json
from pathlib import Path
import tempfile
import unittest

spec = importlib.util.spec_from_file_location("receiver", Path(__file__).with_name("receive-mobile-release.py"))
receiver = importlib.util.module_from_spec(spec)
spec.loader.exec_module(receiver)


class PublicationTest(unittest.TestCase):
    def test_atomic_publication_retry_and_rejections(self):
        apk = b"synthetic APK fixture"
        manifest = dict(package_id="ru.mralexandrov.notes", version_code=3, version_name="synthetic", sha256=hashlib.sha256(apk).hexdigest(), bytes=len(apk), certificate_sha256="a"*64, revision="b"*40, min_sdk=26)
        stream = lambda m, body: io.BytesIO(json.dumps(m).encode()+b"\n"+body)
        with tempfile.TemporaryDirectory() as directory:
            receiver.receive(stream(manifest, apk), directory)
            receiver.receive(stream(manifest, apk), directory)
            original = (Path(directory)/"current.json").read_bytes()
            for changes, body in [({"version_code":2}, apk), ({"version_code":3,"version_name":"different"},apk), ({"version_code":4,"certificate_sha256":"c"*64},apk), ({"version_code":4},apk[:-1]), ({"version_code":4},apk+b"extra"), ({"version_code":4,"sha256":"0"*64},apk), ({"version_code":4,"bytes":receiver.MAX_APK+1},apk)]:
                with self.subTest(changes=changes):
                    with self.assertRaises(ValueError):
                        receiver.receive(stream(dict(manifest,**changes),body),directory)
                    self.assertEqual(original,(Path(directory)/"current.json").read_bytes())


if __name__ == "__main__":
    unittest.main()
