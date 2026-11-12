# -*- coding: utf-8 -*-
"""Filesystem operations, all confined to a root directory.

Every public method takes *relative* paths (as seen by the client) and resolves
them under the configured root using Tools.safe_join, which blocks path
traversal and symlink escapes. Nothing here touches the network or the
encryption layer; it is pure filesystem logic so it can be unit-tested alone.
"""
import fnmatch
import os
import shutil
import stat
import tarfile
import time
import zipfile

from ..util.tools import Tools


class FileOpError(Exception):
    """Raised for expected, user-facing filesystem errors."""


class FileOps(object):
    def __init__(self, root, show_hidden=False, exclude_items=None):
        self.root = os.path.realpath(root)
        self.show_hidden = show_hidden
        self.exclude_items = list(exclude_items or [])

    # -- helpers ----------------------------------------------------------
    def abspath(self, rel):
        try:
            return Tools.safe_join(self.root, rel or '')
        except ValueError:
            raise FileOpError('Invalid path')

    def _hidden(self, name, relpath):
        if not self.show_hidden and name.startswith('.'):
            return True
        for pattern in self.exclude_items:
            if fnmatch.fnmatch(name, pattern) or fnmatch.fnmatch(relpath, pattern):
                return True
        return False

    def _entry(self, abspath, name=None):
        name = name or os.path.basename(abspath)
        try:
            st = os.lstat(abspath)
        except OSError:
            return None
        is_link = stat.S_ISLNK(st.st_mode)
        try:
            tgt = os.stat(abspath) if is_link else st
        except OSError:
            tgt = st
        is_dir = stat.S_ISDIR(tgt.st_mode)
        return {
            'name': name,
            'path': Tools.rel_path(self.root, abspath),
            'is_dir': is_dir,
            'is_link': is_link,
            'size': 0 if is_dir else tgt.st_size,
            'mtime': int(tgt.st_mtime),
            'mtime_str': time.strftime('%Y-%m-%d %H:%M',
                                       time.localtime(tgt.st_mtime)),
            'mode': stat.S_IMODE(tgt.st_mode),
            'perms': stat.filemode(tgt.st_mode),
        }

    # -- read -------------------------------------------------------------
    def listdir(self, rel):
        base = self.abspath(rel)
        if not os.path.isdir(base):
            raise FileOpError('Not a directory')
        dirs, files = [], []
        try:
            names = os.listdir(base)
        except OSError as exc:
            raise FileOpError('Cannot read directory: %s' % exc.strerror)
        for name in names:
            ap = os.path.join(base, name)
            relp = Tools.rel_path(self.root, ap)
            if self._hidden(name, relp):
                continue
            entry = self._entry(ap, name)
            if entry is None:
                continue
            (dirs if entry['is_dir'] else files).append(entry)
        dirs.sort(key=lambda e: e['name'].lower())
        files.sort(key=lambda e: e['name'].lower())
        total = sum(f['size'] for f in files)
        return {
            'path': Tools.rel_path(self.root, base),
            'entries': dirs + files,
            'dir_count': len(dirs),
            'file_count': len(files),
            'total_size': total,
        }

    def stat_entry(self, rel):
        ap = self.abspath(rel)
        if not os.path.exists(ap):
            raise FileOpError('Not found')
        return self._entry(ap)

    def read_text(self, rel, max_bytes=5 * 1024 * 1024):
        ap = self.abspath(rel)
        if not os.path.isfile(ap):
            raise FileOpError('Not a file')
        if os.path.getsize(ap) > max_bytes:
            raise FileOpError('File too large to edit')
        with open(ap, 'rb') as fh:
            data = fh.read()
        try:
            return data.decode('utf-8')
        except UnicodeDecodeError:
            raise FileOpError('File is not valid UTF-8 text')

    # -- write ------------------------------------------------------------
    def write_text(self, rel, content):
        ap = self.abspath(rel)
        parent = os.path.dirname(ap)
        if not os.path.isdir(parent):
            raise FileOpError('Parent directory does not exist')
        with open(ap, 'wb') as fh:
            fh.write(content.encode('utf-8'))
        return self._entry(ap)

    def mkdir(self, rel, name):
        self._validate_name(name)
        ap = self.abspath(os.path.join(rel or '', name))
        if os.path.exists(ap):
            raise FileOpError('Already exists')
        os.makedirs(ap)
        return self._entry(ap)

    def create_file(self, rel, name):
        self._validate_name(name)
        ap = self.abspath(os.path.join(rel or '', name))
        if os.path.exists(ap):
            raise FileOpError('Already exists')
        with open(ap, 'x'):
            pass
        return self._entry(ap)

    def rename(self, rel, new_name):
        self._validate_name(new_name)
        ap = self.abspath(rel)
        if not os.path.exists(ap):
            raise FileOpError('Not found')
        target = os.path.join(os.path.dirname(ap), new_name)
        target = self.abspath(Tools.rel_path(self.root, target))
        if os.path.exists(target):
            raise FileOpError('Target already exists')
        os.rename(ap, target)
        return self._entry(target)

    def delete(self, rel):
        ap = self.abspath(rel)
        if ap == self.root:
            raise FileOpError('Refusing to delete the root')
        if not os.path.exists(ap) and not os.path.islink(ap):
            raise FileOpError('Not found')
        if os.path.isdir(ap) and not os.path.islink(ap):
            shutil.rmtree(ap)
        else:
            os.remove(ap)
        return True

    def _dest_for(self, src_abs, dest_dir_rel):
        dest_dir = self.abspath(dest_dir_rel or '')
        if not os.path.isdir(dest_dir):
            raise FileOpError('Destination is not a directory')
        return os.path.join(dest_dir, os.path.basename(src_abs))

    def copy(self, rel, dest_dir_rel):
        src = self.abspath(rel)
        if not os.path.exists(src):
            raise FileOpError('Source not found')
        dest = self._dest_for(src, dest_dir_rel)
        if os.path.exists(dest):
            raise FileOpError('Target already exists')
        if os.path.isdir(src):
            shutil.copytree(src, dest)
        else:
            shutil.copy2(src, dest)
        return self._entry(dest)

    def move(self, rel, dest_dir_rel):
        src = self.abspath(rel)
        if not os.path.exists(src):
            raise FileOpError('Source not found')
        dest = self._dest_for(src, dest_dir_rel)
        if os.path.exists(dest):
            raise FileOpError('Target already exists')
        shutil.move(src, dest)
        return self._entry(dest)

    def chmod(self, rel, mode):
        ap = self.abspath(rel)
        if not os.path.exists(ap):
            raise FileOpError('Not found')
        try:
            os.chmod(ap, mode & 0o7777)
        except OSError as exc:
            raise FileOpError('chmod failed: %s' % exc.strerror)
        return self._entry(ap)

    # -- archives ---------------------------------------------------------
    def make_zip(self, rels, dest_dir_rel, archive_name=None):
        dest_dir = self.abspath(dest_dir_rel or '')
        name = archive_name or ('archive_%s.zip' % time.strftime('%Y%m%d_%H%M%S'))
        self._validate_name(name)
        out = os.path.join(dest_dir, name)
        if os.path.exists(out):
            raise FileOpError('Archive already exists')
        with zipfile.ZipFile(out, 'w', zipfile.ZIP_DEFLATED) as zf:
            for rel in rels:
                src = self.abspath(rel)
                if os.path.isdir(src):
                    for dp, _dn, fn in os.walk(src):
                        for f in fn:
                            fp = os.path.join(dp, f)
                            arc = os.path.relpath(fp, os.path.dirname(src))
                            zf.write(fp, arc)
                else:
                    zf.write(src, os.path.basename(src))
        return self._entry(out)

    def make_tar(self, rels, dest_dir_rel, archive_name=None):
        dest_dir = self.abspath(dest_dir_rel or '')
        name = archive_name or ('archive_%s.tar.gz' % time.strftime('%Y%m%d_%H%M%S'))
        self._validate_name(name)
        out = os.path.join(dest_dir, name)
        if os.path.exists(out):
            raise FileOpError('Archive already exists')
        with tarfile.open(out, 'w:gz') as tf:
            for rel in rels:
                src = self.abspath(rel)
                tf.add(src, arcname=os.path.basename(src))
        return self._entry(out)

    def extract(self, rel, to_subfolder=False):
        src = self.abspath(rel)
        if not os.path.isfile(src):
            raise FileOpError('Archive not found')
        base_dir = os.path.dirname(src)
        if to_subfolder:
            stem = os.path.basename(src)
            for ext in ('.tar.gz', '.tgz', '.tar', '.zip'):
                if stem.lower().endswith(ext):
                    stem = stem[:-len(ext)]
                    break
            base_dir = os.path.join(base_dir, stem)
            os.makedirs(base_dir, exist_ok=True)
        if zipfile.is_zipfile(src):
            with zipfile.ZipFile(src) as zf:
                self._safe_extract_zip(zf, base_dir)
        elif tarfile.is_tarfile(src):
            with tarfile.open(src) as tf:
                self._safe_extract_tar(tf, base_dir)
        else:
            raise FileOpError('Unsupported archive format')
        return self._entry(base_dir)

    def _safe_extract_zip(self, zf, dest):
        dest_real = os.path.realpath(dest)
        for member in zf.namelist():
            target = os.path.realpath(os.path.join(dest, member))
            if target != dest_real and not target.startswith(dest_real + os.sep):
                raise FileOpError('Unsafe path in archive')
        zf.extractall(dest)

    def _safe_extract_tar(self, tf, dest):
        dest_real = os.path.realpath(dest)
        for member in tf.getmembers():
            target = os.path.realpath(os.path.join(dest, member.name))
            if target != dest_real and not target.startswith(dest_real + os.sep):
                raise FileOpError('Unsafe path in archive')
        tf.extractall(dest)

    # -- search -----------------------------------------------------------
    def search(self, rel, query, limit=500):
        base = self.abspath(rel or '')
        query = (query or '').lower().strip()
        if not query:
            return []
        results = []
        for dp, dns, fns in os.walk(base):
            dns[:] = [d for d in dns
                      if not self._hidden(d, Tools.rel_path(self.root,
                                                            os.path.join(dp, d)))]
            for name in list(dns) + fns:
                ap = os.path.join(dp, name)
                relp = Tools.rel_path(self.root, ap)
                if self._hidden(name, relp):
                    continue
                if query in name.lower():
                    entry = self._entry(ap, name)
                    if entry:
                        results.append(entry)
                        if len(results) >= limit:
                            return results
        return results

    # -- validation -------------------------------------------------------
    @staticmethod
    def _validate_name(name):
        if not name or name in ('.', '..'):
            raise FileOpError('Invalid name')
        if '/' in name or '\\' in name or '\x00' in name:
            raise FileOpError('Invalid name')
