# -*- coding: utf-8 -*-
"""Minimal leveled logger built on top of Color."""
import sys
import time

from .color import Color

_LEVELS = {'debug': 10, 'info': 20, 'warn': 30, 'error': 40}


class Logger(object):
    level = 20  # info

    @staticmethod
    def set_level(name):
        Logger.level = _LEVELS.get(str(name).lower(), 20)

    @staticmethod
    def _ts():
        return time.strftime('%H:%M:%S')

    @staticmethod
    def _emit(threshold, tag, text, stream=sys.stdout):
        if Logger.level > threshold:
            return
        stream.write(Color.s('{GR}%s{W} %s %s\n' % (Logger._ts(), tag, text)))
        stream.flush()

    @staticmethod
    def debug(text):
        Logger._emit(10, Color.s('{P}[dbg]{W}'), text)

    @staticmethod
    def info(text):
        Logger._emit(20, Color.s('{C}[*]{W}'), text)

    @staticmethod
    def ok(text):
        Logger._emit(20, Color.s('{G}[+]{W}'), text)

    @staticmethod
    def warn(text):
        Logger._emit(30, Color.s('{O}[!]{W}'), text)

    @staticmethod
    def error(text):
        Logger._emit(40, Color.s('{R}[-]{W}'), text, stream=sys.stderr)
