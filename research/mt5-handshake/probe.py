"""Explicitly run a single native login against an authorized endpoint."""

from argparse import ArgumentParser
from getpass import getpass
from socket import create_connection, socket
from time import monotonic

from handshake import BUILD_6300, HandshakeError, authenticate


class BoundedConnection:
    def __init__(self, connection: socket, deadline: float) -> None:
        self.connection = connection
        self.deadline = deadline

    def _set_remaining_timeout(self) -> None:
        remaining = self.deadline - monotonic()
        if remaining <= 0:
            raise TimeoutError
        self.connection.settimeout(remaining)

    def sendall(self, data: bytes) -> None:
        self._set_remaining_timeout()
        self.connection.sendall(data)

    def recv(self, size: int) -> bytes:
        self._set_remaining_timeout()
        return self.connection.recv(size)


def main() -> int:
    parser = ArgumentParser(description=__doc__)
    parser.add_argument('host')
    parser.add_argument('port', type=int)
    parser.add_argument('login', type=int)
    parser.add_argument('--direct-extended', action='store_true',
                        help='start with the extended challenge on a fresh TCP connection')
    args = parser.parse_args()

    password = getpass('MT5 investor password: ')
    timeout = 8.0
    deadline = monotonic() + timeout
    try:
        with create_connection((args.host, args.port), timeout=timeout) as connection:
            result = authenticate(BoundedConnection(connection, deadline), args.login, password, BUILD_6300,
                                  direct_extended=args.direct_extended)
    except (HandshakeError, ValueError) as error:
        parser.exit(1, f'{error}\n')
    except TimeoutError:
        parser.exit(1, 'TCP authentication timed out\n')
    except OSError:
        parser.exit(1, 'TCP handshake interrupted\n')
    print(f'Server build {result.server_build}, verified {result.proof_algorithm} proof')
    print('Observed extension descriptors', result.extensions)
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
