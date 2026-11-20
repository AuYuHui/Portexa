"""Validate the account record from a complete native synchronization reply."""

from dataclasses import dataclass
from math import isfinite
from struct import unpack_from

from handshake import HandshakeError


ACCOUNT_SECTION = 0x25
ACCOUNT_RECORD_SIZE = 2996
_INVESTOR_FLAG = 0x08


@dataclass(frozen=True)
class InvestorAccount:
    balance: float
    credit: float


def decode_investor_account(section: bytes, expected_login: int) -> InvestorAccount:
    """Read an isolated account section whose boundary was checked by the caller."""
    if len(section) != 1 + ACCOUNT_RECORD_SIZE or section[0] != ACCOUNT_SECTION:
        raise HandshakeError('invalid native account section')
    record = memoryview(section)[1:]
    received = unpack_from('<Q', record)[0]
    if received == 0 or received != expected_login:
        raise HandshakeError('native account does not match the requested login')
    flags = unpack_from('<I', record, 456)[0]
    if not flags & _INVESTOR_FLAG:
        raise HandshakeError('native investor permission was not granted')
    balance, credit = unpack_from('<dd', record, 1836)
    if not isfinite(balance) or not isfinite(credit):
        raise HandshakeError('native account funds are invalid')
    return InvestorAccount(balance, credit)
