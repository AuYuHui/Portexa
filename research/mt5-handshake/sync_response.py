"""Receive a bounded, complete native synchronization reply."""

from collections.abc import Callable
from struct import unpack_from

from handshake import HandshakeError, PeerClosed, Transport, _HEADER, _read_exact
from stream_state import RollingPayload


_COMPRESSED = 1
_FINAL = 2
_MAX_REPLY_SIZE = 16 << 20
_MAX_FRAMES = 64


class IncompleteSync(HandshakeError):
    def __init__(self, fragments: int, received_bytes: int) -> None:
        super().__init__(f'peer closed initial synchronization after {fragments} fragments ({received_bytes} bytes)')
        self.fragments = fragments
        self.received_bytes = received_bytes


def receive_sync_reply(connection: Transport, state: RollingPayload, message_type: int,
                       sequence: int, decompress: Callable[[bytes], bytes]) -> bytes:
    """Update stream state for each frame and return only a complete matching reply."""
    chunks = []
    received = 0
    for _ in range(_MAX_FRAMES):
        try:
            kind, length, reply_sequence, flags = _HEADER.unpack(_read_exact(connection, _HEADER.size))
            if length > _MAX_REPLY_SIZE:
                raise HandshakeError('initial synchronization frame exceeds size limit')
            if flags & ~(_COMPRESSED | _FINAL):
                raise HandshakeError('unsupported initial synchronization flags')
            frame = _read_exact(connection, length)
        except PeerClosed as error:
            raise IncompleteSync(len(chunks), received) from error

        compressed = bool(flags & _COMPRESSED)
        payload = state.decode(frame, compressed=compressed)
        if (kind, reply_sequence) != (message_type, sequence):
            continue
        if compressed:
            if len(payload) < 8:
                raise HandshakeError('truncated compressed synchronization frame')
            expanded_size, compressed_size = unpack_from('<II', payload)
            if not 0 < expanded_size <= _MAX_REPLY_SIZE - received or compressed_size != len(payload) - 8:
                raise HandshakeError('invalid compressed synchronization lengths')
            payload = decompress(payload)
            if len(payload) != expanded_size:
                raise HandshakeError('decompressed synchronization length does not match')
        received += len(payload)
        if received > _MAX_REPLY_SIZE:
            raise HandshakeError('initial synchronization reply exceeds size limit')
        chunks.append(payload)
        if flags & _FINAL:
            return b''.join(chunks)

    raise HandshakeError('initial synchronization exceeded frame limit')
