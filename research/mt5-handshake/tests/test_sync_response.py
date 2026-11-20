from pathlib import Path
from struct import pack
import sys
from unittest import TestCase

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from handshake import HandshakeError
from stream_state import RollingPayload
from sync_response import IncompleteSync, receive_sync_reply


class ResponseStream:
    def __init__(self, data):
        self.data = data

    def recv(self, size):
        size = min(size, 11)
        block, self.data = self.data[:size], self.data[size:]
        return block

    def sendall(self, data):
        raise AssertionError('receiving the synchronization response wrote to the socket')


def frame(sender, kind, sequence, flags, payload):
    return pack('<BIHH', kind, len(payload), sequence, flags) + sender.encode(payload)


class SyncResponseTests(TestCase):
    def test_rejects_eof_after_two_synchronized_fragments(self):
        table = bytes(range(256))
        sender = RollingPayload(table)
        stream = ResponseStream(frame(sender, 42, 7, 0, b'first') + frame(sender, 42, 7, 0, b'second'))
        with self.assertRaises(IncompleteSync) as raised:
            receive_sync_reply(stream, RollingPayload(table), 42, 7, lambda data: data)
        self.assertEqual((raised.exception.fragments, raised.exception.received_bytes), (2, 11))

    def test_waits_for_matching_final_fragment_and_keeps_stream_state(self):
        table = bytes(range(256))
        sender = RollingPayload(table)
        stream = ResponseStream(
            frame(sender, 42, 7, 0, b'first') +
            frame(sender, 42, 8, 2, b'unrelated') +
            frame(sender, 42, 7, 2, b'final')
        )
        self.assertEqual(receive_sync_reply(stream, RollingPayload(table), 42, 7, lambda data: data),
                         b'firstfinal')

    def test_rejects_oversized_frame_before_reading_payload(self):
        stream = ResponseStream(pack('<BIHH', 42, 32 << 20, 7, 2))
        with self.assertRaisesRegex(HandshakeError, 'frame exceeds size limit'):
            receive_sync_reply(stream, RollingPayload(bytes(256)), 42, 7, lambda data: data)
