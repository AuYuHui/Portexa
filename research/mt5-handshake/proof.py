"""MD5 continuation used by the compatibility field in extended MT5 login."""

from math import sin
from struct import pack, unpack


_MASK = (1 << 32) - 1
_ROUNDS = tuple(int(abs(sin(i)) * (1 << 32)) for i in range(1, 65))
_SHIFTS = ((7, 12, 17, 22), (5, 9, 14, 20), (4, 11, 16, 23), (6, 10, 15, 21))


def legacy_proof(initial_digest: bytes, challenge: bytes) -> bytes:
    """Hash the challenge from the previous digest with a reset length counter."""
    if len(challenge) != 16:
        raise ValueError("expected a 16-byte challenge")
    state = unpack('<4I', initial_digest)
    block = (challenge + b'\x80').ljust(56, b'\0') + pack('<Q', 8 * len(challenge))
    words = unpack('<16I', block)
    a, b, c, d = state
    for step, constant in enumerate(_ROUNDS):
        group = step // 16
        if group == 0:
            mix, index = (b & c) | (~b & d), step
        elif group == 1:
            mix, index = (d & b) | (~d & c), (5 * step + 1) % 16
        elif group == 2:
            mix, index = b ^ c ^ d, (3 * step + 5) % 16
        else:
            mix, index = c ^ (b | ~d), (7 * step) % 16
        shift = _SHIFTS[group][step % 4]
        value = (a + mix + constant + words[index]) & _MASK
        rotated = (value << shift) | (value >> (32 - shift))
        a, b, c, d = d, (b + rotated) & _MASK, b, c
    return pack('<4I', *((initial + final) & _MASK for initial, final in zip(state, (a, b, c, d))))
