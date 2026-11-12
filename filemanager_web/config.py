# -*- coding: utf-8 -*-
"""Global runtime configuration (singleton-style, class-level state)."""
import os

from .__meta__ import __version__, __title__
from .libs.auth import UserStore
from .server.session import SessionManager


class Configuration(object):
    initialized = False
    version = __version__
    title = __title__

    # network
    host = '127.0.0.1'
    port = 8181

    # filesystem
    root_path = os.getcwd()

    # access control
    use_auth = True
    global_readonly = False
    show_hidden = False
    allowed_upload_extensions = None   # None => all allowed; else set() of ext
    exclude_items = []                 # glob patterns hidden from listings
    max_upload_size = 5 * 1024 * 1024 * 1024  # 5 GiB
    upload_chunk_size = 2 * 1024 * 1024       # 2 MiB

    # runtime singletons
    users = None            # UserStore
    sessions = None         # SessionManager
    session_ttl = 3600

    # ui
    app_title = 'FileManager'
    default_theme = 'dark'

    @staticmethod
    def initialize():
        if Configuration.initialized:
            return
        if Configuration.users is None:
            Configuration.users = UserStore()
        if Configuration.sessions is None:
            Configuration.sessions = SessionManager(
                ttl_seconds=Configuration.session_ttl,
            )
        Configuration.root_path = os.path.realpath(Configuration.root_path)
        Configuration.initialized = True

    @staticmethod
    def get_banner():
        return (
            '{C}filemanager-web{W} {GR}v%s{W}  '
            '{GR}stand-alone encrypted web file manager{W}'
            % Configuration.version
        )
