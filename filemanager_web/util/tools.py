# -*- coding: utf-8 -*-
"""Generic helpers: safe path resolution, size formatting, exit handling."""
import os
import sys


class Tools(object):

    @staticmethod
    def exit_gracefully(code=0):
        try:
            sys.stdout.flush()
        except Exception:
            pass
        sys.exit(code)

    @staticmethod
    def human_size(num):
        try:
            num = float(num)
        except (TypeError, ValueError):
            return '-'
        for unit in ('B', 'KB', 'MB', 'GB', 'TB', 'PB'):
            if abs(num) < 1024.0:
                if unit == 'B':
                    return '%d B' % int(num)
                return '%.1f %s' % (num, unit)
            num /= 1024.0
        return '%.1f EB' % num

    @staticmethod
    def safe_join(root, *paths):
        """Join *paths under root, guaranteeing the result stays inside root.

        Returns the absolute, normalized path, or raises ValueError on any
        attempt to escape the root (path traversal) or on absolute components.
        """
        root_abs = os.path.realpath(root)
        rel = os.path.normpath(os.path.join(*paths)) if paths else ''
        # Strip leading separators / drive letters so join cannot escape.
        rel = rel.replace('\\', '/').lstrip('/')
        if rel in ('', '.'):
            return root_abs
        candidate = os.path.normpath(os.path.join(root_abs, rel))
        candidate_real = os.path.realpath(candidate)
        # Containment check on the realpath to defeat symlink escapes.
        if candidate_real != root_abs and not candidate_real.startswith(root_abs + os.sep):
            raise ValueError('Path traversal detected: %r' % rel)
        return candidate_real

    @staticmethod
    def rel_path(root, abs_path):
        root_abs = os.path.realpath(root)
        abs_real = os.path.realpath(abs_path)
        rel = os.path.relpath(abs_real, root_abs)
        if rel == '.':
            return ''
        return rel.replace('\\', '/')
