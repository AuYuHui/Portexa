"""MT5 native TCP authentication experiment for the build 6300 wire profile."""

from dataclasses import dataclass, field
from hashlib import md5, sha256
from hmac import compare_digest
from secrets import token_bytes
from struct import Struct, pack, pack_into, unpack_from
from typing import Protocol

from proof import legacy_proof


_HEADER = Struct('<BIHH')
_EXTENSION = Struct('<BI')
_MAX_MESSAGE = 64 * 1024
_FINAL_FRAGMENT = 2
_BOOTSTRAP_MASK = bytes.fromhex('41b67f58380cf02d7b3908fe21bb4158')
_AUTHORIZATION_STATE_SIZE = 8


class HandshakeError(Exception):
    """The peer's response does not satisfy the selected native profile."""


class PeerClosed(HandshakeError):
    """The peer closed the connection before the expected frame was read."""


class Transport(Protocol):
    def sendall(self, data: bytes) -> None: ...

    def recv(self, size: int) -> bytes: ...


@dataclass(frozen=True)
class WireProfile:
    initial_type: int
    extended_type: int
    proof_type: int
    client_build: int
    protocol_version: int
    client_mode: int
    upgrade_status: int
    minimum_extended_build: int


BUILD_6300 = WireProfile(0, 251, 252, 6300, 0x514D, 4, 1035, 5916)


@dataclass(frozen=True)
class Authentication:
    server_build: int
    proof_algorithm: str
    extensions: tuple[tuple[int, int], ...]
    authorization_state: bytes | None = field(default=None, repr=False)


def bootstrap_transform(data: bytes, *, outbound: bool) -> bytes:
    previous = 0
    result = bytearray(len(data))
    for index, value in enumerate(data):
        masked = value ^ ((previous + _BOOTSTRAP_MASK[index % len(_BOOTSTRAP_MASK)]) & 255)
        result[index] = masked
        previous = masked if outbound else value
    return bytes(result)


def _read_exact(connection: Transport, size: int) -> bytes:
    result = bytearray()
    while len(result) < size:
        block = connection.recv(size - len(result))
        if not block:
            raise PeerClosed('peer closed the connection')
        result.extend(block)
    return bytes(result)


def _exchange(connection: Transport, kind: int, sequence: int, body: bytes) -> bytes:
    connection.sendall(_HEADER.pack(kind, len(body), sequence, _FINAL_FRAGMENT) +
                       bootstrap_transform(body, outbound=True))
    reply_type, size, reply_sequence, flags = _HEADER.unpack(_read_exact(connection, _HEADER.size))
    if reply_type != kind or reply_sequence != sequence:
        raise HandshakeError('reply does not match the expected handshake stage')
    if flags != _FINAL_FRAGMENT or size > _MAX_MESSAGE:
        raise HandshakeError('unsupported handshake frame')
    return bootstrap_transform(_read_exact(connection, size), outbound=False)


def _hello(profile: WireProfile, login: int, instance: bytes, *, extended: bool) -> bytes:
    body = bytearray(128 if extended else 34)
    nonce = token_bytes(1)
    if extended:
        pack_into('<q', body, 0, int.from_bytes(nonce, 'little', signed=True))
    else:
        body[0] = nonce[0]
    mode_offset, build_offset, login_offset, instance_offset = (8, 10, 16, 32) if extended else (1, 2, 6, 14)
    body[mode_offset] = profile.client_mode
    pack_into('<HH', body, build_offset, profile.client_build, profile.protocol_version)
    pack_into('<Q', body, login_offset, login)
    body[instance_offset:instance_offset + 16] = instance
    return bytes(body)


def _extension_descriptors(data: bytes) -> tuple[tuple[int, int], ...]:
    cursor = 0
    items = []
    while cursor < len(data):
        if len(data) - cursor < _EXTENSION.size:
            raise HandshakeError('truncated authentication extension header')
        kind, length = _EXTENSION.unpack_from(data, cursor)
        cursor += _EXTENSION.size
        if length > len(data) - cursor:
            raise HandshakeError('truncated authentication extension body')
        items.append((kind, length))
        cursor += length
    return tuple(items)


def authenticate(connection: Transport, login: int, password: str, profile: WireProfile, *,
                 direct_extended: bool = False, previous_state: bytes | None = None) -> Authentication:
    """Verify the peer. Reuse state only for the same account and authenticated endpoint."""
    if not 0 < login <= (1 << 64) - 1:
        raise ValueError('invalid account number')
    if previous_state is not None and len(previous_state) != _AUTHORIZATION_STATE_SIZE:
        raise ValueError('expected eight bytes of prior authorization state')
    password_data = password.encode('utf-16le')
    if not password_data or len(password_data) > 62 or '\0' in password:
        raise ValueError('invalid password size for this profile')
    nonce = token_bytes(15)
    instance = bytes([sum(nonce) & 255]) + nonce

    sequence = 0
    if not direct_extended:
        discovery = _exchange(connection, profile.initial_type, sequence,
                              _hello(profile, login, instance, extended=False))
        if len(discovery) != 32:
            raise HandshakeError('invalid initial challenge size')
        status, server_build = unpack_from('<IH', discovery, 2)
        if status != profile.upgrade_status or server_build < profile.minimum_extended_build:
            raise HandshakeError(f'unsupported initial handshake status {status} for build {server_build}')
        sequence += 1

    extended = _exchange(connection, profile.extended_type, sequence,
                         _hello(profile, login, instance, extended=True))
    if len(extended) != 102:
        raise HandshakeError('invalid extended challenge size')
    status = unpack_from('<I', extended, 8)[0]
    if status != 0:
        raise HandshakeError(f'extended challenge rejected with status {status}')
    if direct_extended:
        server_build = unpack_from('<H', extended, 12)[0]
        if server_build < profile.minimum_extended_build:
            raise HandshakeError(f'unsupported extended challenge build {server_build}')
    server_challenge = extended[14:30]

    account = pack('<Q', login)
    context = 'MQ'.encode('utf-16le')
    legacy_digest = md5(account + password_data[:32] + context).digest()
    extended_digest = sha256(account + password_data + context).digest()
    request = bytearray(token_bytes(268))
    request[8:24] = legacy_proof(legacy_digest, server_challenge)
    request[24:56] = sha256(extended_digest + server_challenge).digest()
    client_challenge = bytes(request[0x38:0x48])
    request[0x50:0x60] = bytes(16)
    request[0x68:0x70] = previous_state if previous_state is not None else bytes(_AUTHORIZATION_STATE_SIZE)
    request[0x80:0x84] = bytes(4)
    response = _exchange(connection, profile.proof_type, sequence + 1, request)

    if len(response) < 12:
        raise HandshakeError('truncated authentication status')
    status = unpack_from('<I', response, 8)[0]
    if status != 0:
        raise HandshakeError(f'authentication rejected with status {status}')
    if len(response) < 980:
        raise HandshakeError('truncated authentication response')
    capabilities = unpack_from('<Q', response, 12)[0]
    if capabilities & 2:
        algorithm = 'SHA-256'
        expected = sha256(extended_digest + client_challenge).digest()
        actual = response[0x40:0x60]
    else:
        algorithm = 'MD5'
        expected = legacy_proof(legacy_digest, client_challenge)
        actual = response[0x30:0x40]
    if not compare_digest(actual, expected):
        raise HandshakeError('server authentication proof did not match')
    extensions = _extension_descriptors(response[980:])
    state = response[0x2e8:0x2f0]
    return Authentication(server_build, algorithm, extensions, state if any(state) else None)
