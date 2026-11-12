# -*- coding: utf-8 -*-
"""Command-line argument parsing."""
import argparse

from .__meta__ import __version__


def build_parser():
    p = argparse.ArgumentParser(
        prog='filemanager-web',
        description='Stand-alone encrypted web file manager '
                    '(a modern tinyfilemanager built on http.server).',
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog=(
            'examples:\n'
            '  filemanager-web                      serve ./ on 127.0.0.1:8181\n'
            '  filemanager-web -r /data -p 9000     serve /data on port 9000\n'
            '  filemanager-web --user bob --pass s3cret\n'
            '  filemanager-web --bind 0.0.0.0 --no-auth --readonly\n'
        ),
    )
    p.add_argument('-r', '--root', default='.', metavar='DIR',
                   help='directory to serve (default: current directory)')
    p.add_argument('-b', '--bind', default='127.0.0.1', metavar='ADDR',
                   help='address to bind (default: 127.0.0.1)')
    p.add_argument('-p', '--port', type=int, default=8181, metavar='PORT',
                   help='TCP port (default: 8181)')

    auth = p.add_argument_group('authentication')
    auth.add_argument('--user', metavar='NAME',
                      help='username (default: admin)')
    auth.add_argument('--pass', dest='password', metavar='PASS',
                      help='password (default: auto-generated and printed once)')
    auth.add_argument('--no-auth', action='store_true',
                      help='disable authentication entirely')
    auth.add_argument('--readonly', action='store_true',
                      help='serve read-only (no modifications allowed)')
    auth.add_argument('--session-ttl', type=int, default=3600, metavar='SEC',
                      help='idle session timeout in seconds (default: 3600)')

    disp = p.add_argument_group('display / filtering')
    disp.add_argument('--show-hidden', action='store_true',
                      help='show dotfiles and hidden entries')
    disp.add_argument('--title', default='FileManager', metavar='TEXT',
                      help='application title shown in the UI')
    disp.add_argument('--exclude', action='append', default=[], metavar='GLOB',
                      help='glob pattern to hide from listings (repeatable)')

    misc = p.add_argument_group('misc')
    misc.add_argument('-v', '--verbose', action='count', default=0,
                      help='increase verbosity (-vv for debug)')
    misc.add_argument('-q', '--quiet', action='store_true',
                      help='only log warnings and errors')
    misc.add_argument('-V', '--version', action='version',
                      version='filemanager-web %s' % __version__)
    return p


def parse_args(argv=None):
    return build_parser().parse_args(argv)
