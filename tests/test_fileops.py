# -*- coding: utf-8 -*-
import os
import tempfile
import unittest

from filemanager_web.libs.fileops import FileOps, FileOpError


class FileOpsTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.mkdtemp()
        self.fo = FileOps(self.tmp)

    def test_mkdir_touch_list(self):
        self.fo.mkdir('', 'sub')
        self.fo.create_file('sub', 'a.txt')
        res = self.fo.listdir('sub')
        names = [e['name'] for e in res['entries']]
        self.assertIn('a.txt', names)
        self.assertEqual(res['file_count'], 1)

    def test_write_read_roundtrip(self):
        self.fo.create_file('', 'f.txt')
        self.fo.write_text('f.txt', 'hello world')
        self.assertEqual(self.fo.read_text('f.txt'), 'hello world')

    def test_rename_delete(self):
        self.fo.create_file('', 'a.txt')
        self.fo.rename('a.txt', 'b.txt')
        self.assertTrue(os.path.exists(os.path.join(self.tmp, 'b.txt')))
        self.fo.delete('b.txt')
        self.assertFalse(os.path.exists(os.path.join(self.tmp, 'b.txt')))

    def test_traversal_blocked(self):
        with self.assertRaises(FileOpError):
            self.fo.read_text('../../etc/passwd')
        with self.assertRaises(FileOpError):
            self.fo.mkdir('..', 'evil')

    def test_invalid_names(self):
        with self.assertRaises(FileOpError):
            self.fo.mkdir('', '../x')
        with self.assertRaises(FileOpError):
            self.fo.create_file('', 'a/b')

    def test_zip_and_extract(self):
        self.fo.mkdir('', 'src')
        self.fo.create_file('src', 'x.txt')
        self.fo.write_text('src/x.txt', 'data')
        self.fo.make_zip(['src'], '', 'out.zip')
        self.assertTrue(os.path.exists(os.path.join(self.tmp, 'out.zip')))
        self.fo.extract('out.zip', to_subfolder=True)
        self.assertTrue(os.path.isdir(os.path.join(self.tmp, 'out')))

    def test_search(self):
        self.fo.create_file('', 'findme.txt')
        res = self.fo.search('', 'findme')
        self.assertTrue(any(e['name'] == 'findme.txt' for e in res))


if __name__ == '__main__':
    unittest.main()
