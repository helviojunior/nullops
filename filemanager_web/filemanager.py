# -*- coding: utf-8 -*-
"""Application entry point: wire CLI args into Configuration and serve."""
import os
import sys

from .args import parse_args
from .config import Configuration
from .libs.auth import generate_password
from .server.httpd import serve
from .util.color import Color
from .util.logger import Logger


def _setup_logging(args):
    if args.quiet:
        Logger.set_level('warn')
    elif args.verbose >= 2:
        Logger.set_level('debug')
    elif args.verbose == 1:
        Logger.set_level('info')
    else:
        Logger.set_level('info')


def _configure(args):
    Configuration.host = args.bind
    Configuration.port = args.port
    Configuration.root_path = os.path.abspath(args.root)
    Configuration.use_auth = not args.no_auth
    Configuration.global_readonly = bool(args.readonly)
    Configuration.show_hidden = bool(args.show_hidden)
    Configuration.exclude_items = list(args.exclude or [])
    Configuration.app_title = args.title
    Configuration.session_ttl = args.session_ttl
    Configuration.initialize()

    credentials = None
    if Configuration.use_auth:
        username = args.user or 'admin'
        password = args.password or generate_password()
        generated = args.password is None
        Configuration.users.add(username, password=password,
                                readonly=Configuration.global_readonly)
        credentials = (username, password, generated)
    return credentials


def _print_startup(credentials):
    host = Configuration.host
    shown_host = host if host not in ('0.0.0.0', '::') else '127.0.0.1'
    url = 'http://%s:%d/' % (shown_host, Configuration.port)

    Color.pl('')
    Color.pl('  {C}filemanager-web{W} {GR}v%s{W}' % Configuration.version)
    Color.pl('  {GR}serving{W} {W}%s{W}' % Configuration.root_path)
    if Configuration.global_readonly:
        Color.pl('  {O}read-only mode{W}')
    Color.pl('')
    Color.pl('  {+}URL:{W}  {B}%s{W}' % url)
    if Configuration.host in ('0.0.0.0', '::'):
        Color.pl('  {GR}(listening on all interfaces: %s:%d){W}'
                 % (Configuration.host, Configuration.port))
    if credentials is not None:
        user, pwd, generated = credentials
        Color.pl('')
        Color.pl('  {+}user:{W} {G}%s{W}' % user)
        if generated:
            Color.pl('  {+}pass:{W} {G}%s{W}  {GR}(auto-generated){W}' % pwd)
        else:
            Color.pl('  {+}pass:{W} {GR}(as provided){W}')
    else:
        Color.pl('')
        Color.pl('  {O}authentication disabled{W}')
    Color.pl('')
    Color.pl('  {GR}transport: ECDH P-256 + AES-256-GCM per session '
             '(keys in memory only){W}')
    Color.pl('  {GR}press Ctrl+C to stop{W}')
    Color.pl('')


def main(argv=None):
    args = parse_args(argv)
    _setup_logging(args)

    if not os.path.isdir(os.path.abspath(args.root)):
        Logger.error('Root directory does not exist: %s' % args.root)
        return 2

    credentials = _configure(args)
    _print_startup(credentials)

    try:
        return serve(Configuration)
    except OSError as exc:
        Logger.error('Could not start server: %s' % exc)
        return 1


def run():
    try:
        sys.exit(main())
    except KeyboardInterrupt:
        Color.pl('\n  {GR}stopped{W}')
        sys.exit(0)


if __name__ == '__main__':
    run()
