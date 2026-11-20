"""Rolling payload transform observed in MT5 build 6300 after authentication."""


def stream_seed(verified_response: bytes) -> bytes:
    """Read the 256-byte table input from a verified extended auth response."""
    if len(verified_response) < 980:
        raise ValueError('truncated extended authorization response')
    return verified_response[0xE0:0x1E0]


class RollingPayload:
    """Use a separate state per direction. The frame header stays unchanged."""

    def __init__(self, table: bytes) -> None:
        if len(table) != 256:
            raise ValueError('expected a 256-byte session table')
        self._table = bytes(table)
        self._offset = 0
        self._previous = 0

    def encode(self, data: bytes) -> bytes:
        """Encode one outbound payload, retaining its state for the next frame."""
        result = bytearray(len(data))
        previous = self._previous
        for i, value in enumerate(data):
            result[i] = value ^ ((self._table[(self._offset + i) & 255] + previous) & 255)
            previous = value
        self._advance(len(data), previous)
        return bytes(result)

    def decode(self, data: bytes, *, compressed: bool = False) -> bytes:
        """Reverse the received payload transform before optional decompression."""
        result = bytearray(data)
        size = len(result)
        table = self._table
        offset = self._offset
        previous = self._previous
        split = (size + 1) // 2 if compressed and size >= 2 else size
        backward = 0
        for i in range(size - 1, split - 1, -1):
            backward = ((backward ^ data[i]) - table[(offset + i) & 255]) & 255
            result[i - 1] = backward
        for i in range(split):
            previous = ((table[(offset + i) & 255] + previous) & 255) ^ data[i]
            result[i] = previous
        if split < size:
            result[-1] = 0
            if previous == backward:
                previous = 0
            else:
                for i in range(split, size):
                    key = table[(offset + i) & 255]
                    previous_backward = backward
                    backward = result[i]
                    previous = ((key + previous_backward) & 255) ^ ((key + previous) & 255) ^ backward
                    result[i] = previous
        self._advance(size, previous)
        return bytes(result)

    def _advance(self, size: int, previous: int) -> None:
        self._offset = (self._offset + size) & 255
        self._previous = previous
