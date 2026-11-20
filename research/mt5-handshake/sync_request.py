"""Build the cold-cache request body from MT5 build 6300's sync path."""

from dataclasses import dataclass
from hashlib import md5
from struct import pack


_INSTALLATION_EPOCH = 1_420_070_400
_EMPTY_CACHE_DIGEST = md5(b'').digest()


@dataclass(frozen=True)
class ClientDeclaration:
    package: str
    os_version: str
    executable: str
    build: int
    signing_team: str | None = None

    def encode(self) -> bytes:
        entries = [('package', self.package), ('os_ver', self.os_version)]
        if self.signing_team is not None:
            entries.append(('devid', self.signing_team))
        entries.extend((('file', self.executable), ('version', str(self.build))))
        if any(not value or '\t' in value or '\0' in value for _, value in entries):
            raise ValueError('invalid client declaration value')
        text = ''.join(f'{key}={value}\t' for key, value in entries)
        return (text + '\0').encode('utf-16le')


def installation_number(instance_hash: int, creation_time: int) -> int:
    """Encode a locally created instance number using its Unix creation time."""
    if not 0 <= instance_hash < (1 << 25):
        raise ValueError('expected a 25-bit instance hash')
    elapsed = creation_time - _INSTALLATION_EPOCH
    if not 0 <= elapsed < (1 << 32):
        raise ValueError('installation creation time out of range')
    return (0x44 << 56) | (instance_hash << 32) | elapsed


def cold_sync_body(*, instance: int | None, sync_hash: bytes,
                   confirmation: int | None, declaration: ClientDeclaration | None = None,
                   include_news: bool = False) -> bytes:
    """Serialize an empty-cache view with selected session inputs."""
    if len(sync_hash) != 16:
        raise ValueError('expected a 16-byte synchronization hash')
    fields = [(0x22, pack('<Q', 0x3FC))]
    if instance is not None:
        fields.append((0x6C, pack('<Q', instance)))
    fields.extend((
        (0x16, pack('<BQ', int(include_news), 0)),
        (0x18, bytes(8)),
        (0x84, b''),
    ))
    if declaration is not None:
        fields.append((0x7F, declaration.encode()))
    fields.extend((
        (0x07, bytes(12) + _EMPTY_CACHE_DIGEST * 2),
        (0x28, bytes(12) + _EMPTY_CACHE_DIGEST),
        (0x11, sync_hash),
    ))
    if confirmation is not None:
        fields.append((0x58, pack('<Q', confirmation)))
    return b''.join(pack('<BI', kind, len(value)) + value for kind, value in fields)
