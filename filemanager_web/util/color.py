# -*- coding: utf-8 -*-
"""Tiny ANSI color helper (no external deps).

Usage:
    Color.pl('{G}ok{W} done')         -> prints with newline
    Color.s('{R}error{W}')            -> returns colored string
Markup tokens: {W}=reset {R}=red {G}=green {O}=orange/yellow {B}=blue
               {P}=purple {C}=cyan {GR}=grey {D}=dim {+}=bold
Colors are disabled automatically when stdout is not a TTY or NO_COLOR is set.
"""
import os
import sys

_CODES = {
    '{W}': '\033[0m',
    '{+}': '\033[1m',
    '{D}': '\033[2m',
    '{R}': '\033[31m',
    '{G}': '\033[32m',
    '{O}': '\033[33m',
    '{B}': '\033[34m',
    '{P}': '\033[35m',
    '{C}': '\033[36m',
    '{GR}': '\033[90m',
}


def _enabled():
    if os.environ.get('NO_COLOR') is not None:
        return False
    if os.environ.get('FM_NO_COLOR') is not None:
        return False
    return sys.stdout.isatty()


class Color(object):
    use_color = _enabled()

    @staticmethod
    def s(text):
        enabled = Color.use_color
        for token, code in _CODES.items():
            text = text.replace(token, code if enabled else '')
        return text

    @staticmethod
    def pl(text):
        sys.stdout.write(Color.s(text) + Color.s('{W}') + '\n')
        sys.stdout.flush()

    @staticmethod
    def p(text):
        sys.stdout.write(Color.s(text))
        sys.stdout.flush()
