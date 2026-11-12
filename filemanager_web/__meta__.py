# -*- coding: utf-8 -*-
#
# Single source of truth for package metadata and version.
#
# This module intentionally performs NO imports so it can be safely read/exec'd
# by the build backend without importing the whole package.
#
# Versioning convention (mirrors helviojunior/knowsmore):
#   __version__ stays at '0.0.0' in the repository. The build/release process
#   stamps a real PEP 440 version. __build__ is a hex timestamp bumped by a
#   git hook on push.
#
__version__ = '0.0.0'
__title__ = 'filemanager-web'
__package__ = 'filemanager_web'
__description__ = (
    'A modern, stand-alone, single-binary web file manager. A rewrite and '
    'improvement of tinyfilemanager, powered only by the Python standard '
    'library http.server, with an encrypted /api transport (ECDH + AES-GCM).'
)
__url__ = 'https://github.com/helviojunior/filemanager'
__build__ = 0x000001
__author__ = 'Helvio Junior (M4v3r1ck)'
__author_email__ = 'helvio.junior@sec4us.com.br'
__license__ = 'GPL-3.0'
__copyright__ = 'Copyright Helvio Junior'
