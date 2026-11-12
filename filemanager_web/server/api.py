# -*- coding: utf-8 -*-
"""API action dispatcher.

Works on already-decrypted request dicts and returns plain response dicts; the
HTTP handler is responsible for the encryption envelope, session lookup and
binary download/upload streaming. Keeping this layer transport-agnostic makes
it unit-testable without sockets or crypto.
"""
from ..libs.fileops import FileOps, FileOpError


class ApiError(Exception):
    def __init__(self, message, status=400):
        super().__init__(message)
        self.status = status


# actions that mutate the filesystem (blocked for read-only sessions)
_WRITE_ACTIONS = frozenset({
    'save', 'mkdir', 'touch', 'rename', 'delete',
    'copy', 'move', 'chmod', 'zip', 'tar', 'extract',
})


class Api(object):
    def __init__(self, config):
        self.config = config

    def _fileops(self):
        return FileOps(
            self.config.root_path,
            show_hidden=self.config.show_hidden,
            exclude_items=self.config.exclude_items,
        )

    def dispatch(self, action, payload, session):
        """Return a response dict for a JSON action. Raises ApiError."""
        handler = getattr(self, 'do_' + action, None)
        if handler is None:
            raise ApiError('Unknown action: %s' % action, status=404)

        if action in _WRITE_ACTIONS:
            if self.config.global_readonly or (session and session.readonly):
                raise ApiError('Read-only mode', status=403)

        fo = self._fileops()
        try:
            return handler(fo, payload or {}, session)
        except FileOpError as exc:
            raise ApiError(str(exc), status=400)

    # -- read actions -----------------------------------------------------
    def do_list(self, fo, p, session):
        return fo.listdir(p.get('path', ''))

    def do_read(self, fo, p, session):
        path = p.get('path', '')
        return {'path': path, 'content': fo.read_text(path)}

    def do_stat(self, fo, p, session):
        return fo.stat_entry(p.get('path', ''))

    def do_search(self, fo, p, session):
        return {'results': fo.search(p.get('path', ''), p.get('query', ''))}

    def do_info(self, fo, p, session):
        return {
            'username': session.username if session else None,
            'readonly': bool(self.config.global_readonly
                             or (session and session.readonly)),
            'root': self.config.root_path,
            'title': self.config.app_title,
            'version': self.config.version,
            'auth': self.config.use_auth,
        }

    # -- write actions ----------------------------------------------------
    def do_save(self, fo, p, session):
        return fo.write_text(p.get('path', ''), p.get('content', ''))

    def do_mkdir(self, fo, p, session):
        return fo.mkdir(p.get('path', ''), p.get('name', ''))

    def do_touch(self, fo, p, session):
        return fo.create_file(p.get('path', ''), p.get('name', ''))

    def do_rename(self, fo, p, session):
        return fo.rename(p.get('path', ''), p.get('name', ''))

    def do_delete(self, fo, p, session):
        paths = p.get('paths') or [p.get('path', '')]
        for path in paths:
            fo.delete(path)
        return {'deleted': paths}

    def do_copy(self, fo, p, session):
        paths = p.get('paths') or [p.get('path', '')]
        dest = p.get('dest', '')
        out = [fo.copy(path, dest) for path in paths]
        return {'copied': out}

    def do_move(self, fo, p, session):
        paths = p.get('paths') or [p.get('path', '')]
        dest = p.get('dest', '')
        out = [fo.move(path, dest) for path in paths]
        return {'moved': out}

    def do_chmod(self, fo, p, session):
        mode = p.get('mode')
        if isinstance(mode, str):
            mode = int(mode, 8)
        return fo.chmod(p.get('path', ''), int(mode))

    def do_zip(self, fo, p, session):
        return fo.make_zip(p.get('paths') or [], p.get('dest', ''),
                           p.get('name'))

    def do_tar(self, fo, p, session):
        return fo.make_tar(p.get('paths') or [], p.get('dest', ''),
                           p.get('name'))

    def do_extract(self, fo, p, session):
        return fo.extract(p.get('path', ''), bool(p.get('to_subfolder')))
