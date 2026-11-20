from dataclasses import replace
from hashlib import md5, sha256
from pathlib import Path
from struct import Struct, pack, pack_into, unpack_from
import sys
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from handshake import BUILD_6300, HandshakeError, _hello, authenticate, bootstrap_transform
from proof import legacy_proof


_HEADER = Struct('<BIHH')
_ACCOUNT = 1237894
_PASSWORD = '示例-password'
_BASE = pack('<Q', _ACCOUNT) + (_PASSWORD + 'MQ').encode('utf-16le')
_CHALLENGE = bytes(range(16))


class BrokerFixture:
    def __init__(self, test, profile, *, sha_branch=True, bad_proof=False, wrong_reply=False,
                 malformed_extension=False, build=7000, direct_extended=False,
                 expected_state=bytes(8), issued_state=bytes(8)):
        self.test = test
        self.profile = profile
        self.sha_branch = sha_branch
        self.bad_proof = bad_proof
        self.wrong_reply = wrong_reply
        self.malformed_extension = malformed_extension
        self.build = build
        self.expected_state = expected_state
        self.issued_state = issued_state
        self.extended_sequence = 0 if direct_extended else 1
        self.sequence = 0
        self.queue = b''

    def sendall(self, frame):
        kind, length, sequence, flags = _HEADER.unpack_from(frame)
        self.test.assertEqual((length, sequence, flags), (len(frame) - _HEADER.size, self.sequence, 2))
        payload = bootstrap_transform(frame[_HEADER.size:], outbound=False)

        if sequence < self.extended_sequence:
            self.test.assertEqual(kind, self.profile.initial_type)
            self.test.assertEqual(len(payload), 34)
            self.test.assertEqual(unpack_from('<Q', payload, 6)[0], _ACCOUNT)
            reply = bytearray(32)
            pack_into('<IH', reply, 2, self.profile.upgrade_status, self.build)
        elif sequence == self.extended_sequence:
            self.test.assertEqual(kind, self.profile.extended_type)
            self.test.assertEqual(len(payload), 128)
            self.test.assertEqual(unpack_from('<Q', payload, 16)[0], _ACCOUNT)
            reply = bytearray(102)
            pack_into('<H', reply, 12, self.build)
            reply[14:30] = _CHALLENGE
        else:
            self.test.assertEqual(kind, self.profile.proof_type)
            self.test.assertEqual(len(payload), 268)
            self.test.assertEqual(payload[8:24], legacy_proof(md5(_BASE).digest(), _CHALLENGE))
            self.test.assertEqual(payload[24:56], sha256(sha256(_BASE).digest() + _CHALLENGE).digest())
            self.test.assertEqual(payload[0x50:0x60], bytes(16))
            self.test.assertEqual(payload[0x68:0x70], self.expected_state)
            self.test.assertEqual(payload[0x80:0x84], bytes(4))
            reply = bytearray(980)
            challenge = payload[0x38:0x48]
            if self.sha_branch:
                pack_into('<Q', reply, 12, 2)
                reply[0x40:0x60] = sha256(sha256(_BASE).digest() + challenge).digest()
            else:
                reply[0x30:0x40] = legacy_proof(md5(_BASE).digest(), challenge)
            if self.bad_proof:
                reply[0x40] ^= 1
            reply[0x2e8:0x2f0] = self.issued_state
            reply += pack('<BI', 99, 2 if self.malformed_extension else 1) + b'x'

        if self.wrong_reply:
            sequence += 1
        self.queue = _HEADER.pack(kind, len(reply), sequence, 2) + bootstrap_transform(reply, outbound=True)
        self.sequence += 1

    def recv(self, size):
        size = min(size, 13)
        result, self.queue = self.queue[:size], self.queue[size:]
        return result


class HandshakeTests(unittest.TestCase):
    def test_extended_hello_sign_extends_the_random_byte(self):
        for nonce, expected_padding in ((0x7f, 0x00), (0x80, 0xff)):
            with self.subTest(nonce=nonce), patch('handshake.token_bytes', return_value=bytes([nonce])):
                hello = _hello(BUILD_6300, _ACCOUNT, bytes(16), extended=True)
                self.assertEqual(hello[:8], bytes([nonce]) + bytes([expected_padding]) * 7)
                initial = _hello(BUILD_6300, _ACCOUNT, bytes(16), extended=False)
                self.assertEqual(initial[:2], bytes([nonce, BUILD_6300.client_mode]))

    def test_md5_core_matches_standard_with_initial_iv(self):
        iv = pack('<4I', 0x67452301, 0xEFCDAB89, 0x98BADCFE, 0x10325476)
        self.assertEqual(legacy_proof(iv, _CHALLENGE), md5(_CHALLENGE).digest())

    def test_proof_uses_profile_message_types(self):
        renumbered = replace(BUILD_6300, initial_type=97, extended_type=98, proof_type=99,
                             upgrade_status=1578)
        for profile in (BUILD_6300, renumbered):
            with self.subTest(profile=profile):
                result = authenticate(BrokerFixture(self, profile), _ACCOUNT, _PASSWORD, profile)
                self.assertEqual(result.proof_algorithm, 'SHA-256')
                self.assertEqual(result.server_build, 7000)
                self.assertEqual(result.extensions, ((99, 1),))

    def test_sha_and_legacy_server_proof_selection(self):
        broker = BrokerFixture(self, BUILD_6300, sha_branch=False)
        result = authenticate(broker, _ACCOUNT, _PASSWORD, BUILD_6300)
        self.assertEqual(result.proof_algorithm, 'MD5')

    def test_direct_extended_connection_negotiates_build_and_verifies_proof(self):
        profile = replace(BUILD_6300, extended_type=97, proof_type=98)
        broker = BrokerFixture(self, profile, direct_extended=True)
        result = authenticate(broker, _ACCOUNT, _PASSWORD, profile, direct_extended=True)
        self.assertEqual((result.server_build, result.proof_algorithm, broker.sequence), (7000, 'SHA-256', 2))
        old_broker = BrokerFixture(self, profile, direct_extended=True, build=5000)
        with self.assertRaisesRegex(HandshakeError, 'unsupported extended challenge build'):
            authenticate(old_broker, _ACCOUNT, _PASSWORD, profile, direct_extended=True)

    def test_verified_authorization_state_can_be_used_on_next_connection(self):
        previous = b'prior123'
        next_state = b'fresh456'
        initial = BrokerFixture(self, BUILD_6300, direct_extended=True, issued_state=previous)
        first = authenticate(initial, _ACCOUNT, _PASSWORD, BUILD_6300, direct_extended=True)
        self.assertEqual(first.authorization_state, previous)
        reconnect = BrokerFixture(self, BUILD_6300, direct_extended=True,
                                  expected_state=previous, issued_state=next_state)
        second = authenticate(reconnect, _ACCOUNT, _PASSWORD, BUILD_6300, direct_extended=True,
                              previous_state=first.authorization_state)
        self.assertEqual(second.authorization_state, next_state)
        self.assertNotIn('fresh456', repr(second))

    def test_rejects_invalid_response(self):
        for options, error in (({'bad_proof': True}, 'proof did not match'),
                               ({'wrong_reply': True}, 'expected handshake stage'),
                               ({'build': 5000}, 'unsupported initial handshake'),
                               ({'malformed_extension': True}, 'truncated authentication extension body')):
            with self.subTest(options=options), self.assertRaisesRegex(HandshakeError, error):
                broker = BrokerFixture(self, BUILD_6300, **options)
                authenticate(broker, _ACCOUNT, _PASSWORD, BUILD_6300)


if __name__ == '__main__':
    unittest.main()
