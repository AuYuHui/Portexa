from pathlib import Path
from struct import unpack_from
import sys
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from sync_request import ClientDeclaration, cold_sync_body, installation_number


class SynchronizationRequestTests(unittest.TestCase):
    def test_cold_cache_matches_official_builder(self):
        # Measured with empty caches and no optional session inputs.
        expected = bytes.fromhex('''
            2208000000 fc03000000000000
            1609000000 000000000000000000
            1808000000 0000000000000000
            8400000000
            072c000000 000000000000000000000000
                       d41d8cd98f00b204e9800998ecf8427e d41d8cd98f00b204e9800998ecf8427e
            281c000000 000000000000000000000000 d41d8cd98f00b204e9800998ecf8427e
            1110000000 00000000000000000000000000000000
        ''')
        self.assertEqual(cold_sync_body(instance=None, sync_hash=bytes(16), confirmation=None), expected)

    def test_optional_session_values_match_official_builder(self):
        # Same fixture with a synthetic install ID, hash and server confirmation.
        expected = bytes.fromhex('''
            2208000000 fc03000000000000
            6c08000000 0063af109d300744
            1609000000 000000000000000000
            1808000000 0000000000000000
            8400000000
            072c000000 000000000000000000000000
                       d41d8cd98f00b204e9800998ecf8427e d41d8cd98f00b204e9800998ecf8427e
            281c000000 000000000000000000000000 d41d8cd98f00b204e9800998ecf8427e
            1110000000 000102030405060708090a0b0c0d0e0f
            5808000000 7a44093217509863
        ''')
        instance = installation_number(471197, 1_700_000_000)
        body = cold_sync_body(instance=instance, sync_hash=bytes(range(16)), confirmation=0x639850173209447a)
        self.assertEqual(body, expected)

    def test_client_declaration_uses_the_observed_field_order(self):
        declaration = ClientDeclaration('org.example.client', 'Synthetic OS', 'test-client', 7000)
        body = cold_sync_body(instance=1, sync_hash=bytes(16), confirmation=2,
                              include_news=True, declaration=declaration)
        pairs = []
        pos = 0
        while pos < len(body):
            tag, size = unpack_from('<BI', body, pos)
            pos += 5
            pairs.append((tag, body[pos:pos+size]))
            pos += size
        self.assertEqual([tag for tag, _ in pairs], [0x22, 0x6C, 0x16, 0x18, 0x84, 0x7F, 7, 0x28, 0x11, 0x58])
        fields = dict(pairs)
        self.assertEqual(fields[0x16], b'\x01' + bytes(8))
        self.assertEqual(fields[0x7F].decode('utf-16le'),
                         'package=org.example.client\tos_ver=Synthetic OS\tfile=test-client\tversion=7000\t\0')

    def test_signed_client_declaration_includes_its_own_team(self):
        declaration = ClientDeclaration('org.example.client', 'Synthetic OS', 'test-client', 7000,
                                        signing_team='ABC1234567')
        self.assertEqual(declaration.encode().decode('utf-16le'),
                         'package=org.example.client\tos_ver=Synthetic OS\tdevid=ABC1234567\tfile=test-client\tversion=7000\t\0')
