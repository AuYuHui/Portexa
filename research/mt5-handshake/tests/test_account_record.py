from pathlib import Path
from struct import pack_into
import sys
from unittest import TestCase

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from account_record import ACCOUNT_RECORD_SIZE, ACCOUNT_SECTION, decode_investor_account
from handshake import HandshakeError


def section(login=1237894, flags=8, balance=925.75):
    record = bytearray(1 + ACCOUNT_RECORD_SIZE)
    record[0] = ACCOUNT_SECTION
    pack_into('<Q', record, 1, login)
    pack_into('<I', record, 457, flags)
    pack_into('<dd', record, 1837, balance, 10.25)
    return bytes(record)


class AccountRecordTests(TestCase):
    def test_reads_funds_only_after_verifying_account_and_investor_flag(self):
        account = decode_investor_account(section(), 1237894)
        self.assertEqual((account.balance, account.credit), (925.75, 10.25))
        for content, error in (
            (section(login=6789), 'does not match'),
            (section(flags=4), 'investor permission'),
            (section(balance=float('nan')), 'funds are invalid'),
            (section()[:-1], 'invalid native account section'),
        ):
            with self.subTest(error=error), self.assertRaisesRegex(HandshakeError, error):
                decode_investor_account(content, 1237894)
