from pathlib import Path
import sys
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from stream_state import RollingPayload, stream_seed


class StreamStateTests(unittest.TestCase):
    def test_fixed_authorization_material_does_not_require_optional_fields(self):
        response = bytearray(980)
        response[0xE0:0x1E0] = bytes(range(256))
        self.assertEqual(stream_seed(bytes(response)), bytes(range(256)))
        with self.assertRaisesRegex(ValueError, 'truncated'):
            stream_seed(bytes(response[:979]))

    def test_outbound_state_survives_fragment_and_empty_keepalive(self):
        table = bytes(range(256))
        outbound, inbound = RollingPayload(table), RollingPayload(table)
        first = bytes.fromhex('660e190603')
        second = bytes.fromhex('0a1c0f04161c')
        self.assertEqual(outbound.encode(b'first'), first)
        self.assertEqual(outbound.encode(b''), b'')
        self.assertEqual(outbound.encode(b'second'), second)
        self.assertEqual(inbound.decode(first), b'first')
        self.assertEqual(inbound.decode(second), b'second')

    def test_compressed_frame_keeps_inbound_state_for_next_frame(self):
        inbound = RollingPayload(bytes(range(256)))
        frame = bytes.fromhex('0011aaffeedd112277668899ccbbaa77782211ff000ddd')
        expected = bytes.fromhex('0010b844a6766d562954d67848ee56125a494aa2b6c601')
        self.assertEqual(inbound.decode(frame, compressed=True), expected)
        self.assertEqual(inbound.decode(bytes.fromhex('123456789abc')), bytes.fromhex('0a1679eb9c04'))
