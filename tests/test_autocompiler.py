import json
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch
sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'src'))
import autocompiler as ac

class CompilerTests(unittest.TestCase):
    def test_contract(self):
        for obj in ({}, {'analysis':3,'review':'','action':'execute','script':'x','summary':''}):
            with self.assertRaises(ValueError): ac.validate_contract(obj)

    def test_policy_rejects_code_and_escape(self):
        with tempfile.TemporaryDirectory() as tmp:
            for code in ['import os; os.system("echo bad")', 'from pathlib import Path\nPath("../escape").write_text("x",encoding="utf-8")', 'from pathlib import Path\nPath("x").write_text(str(1),encoding="utf-8")', 'from pathlib import Path\nPath("x").write_text("x",encoding="utf-8"); Path("x").write_text("y",encoding="utf-8")']:
                with self.subTest(code=code), self.assertRaises(ValueError):
                    ac.validate_script(code,Path(tmp),'write_text')

    def test_script_executes_and_verifies(self):
        with tempfile.TemporaryDirectory() as tmp:
            root=Path(tmp); work=root/'workspace'; work.mkdir(); attempt=root/'attempt'; attempt.mkdir()
            result=ac.execute({'policy':'write_text','script_timeout':10},{'script':'from pathlib import Path\nPath("hello.txt").write_text("Hello World",encoding="utf-8")'},work,attempt)
            self.assertTrue(result['success'])
            self.assertEqual((work/'hello.txt').read_bytes(),b'Hello World')
            with self.assertRaises(ValueError):
                ac.validate_script('Path("hello.txt").write_text("overwrite",encoding="utf-8")',work,'write_text')

    def test_timeout(self):
        with tempfile.TemporaryDirectory() as tmp:
            root=Path(tmp); work=root/'workspace'; work.mkdir(); attempt=root/'attempt'; attempt.mkdir()
            result=ac.execute({'policy':'trusted_python','script_timeout':1},{'script':'import time; time.sleep(30)'},work,attempt)
            self.assertTrue(result['timed_out']); self.assertFalse(result['success'])

    def test_cannot_finish_without_execution(self):
        with tempfile.TemporaryDirectory() as tmp:
            with patch.object(ac,'infer',return_value={'action':'finish','summary':'claimed success'}):
                state=ac.run({'max_attempts':2,'policy':'write_text'},'test',Path(tmp)/'run')
            self.assertEqual(state['status'],'failed')

    def test_repeated_script_stops(self):
        with tempfile.TemporaryDirectory() as tmp:
            artifact={'action':'execute','script':'x','summary':'repeat'}
            with patch.object(ac,'infer',return_value=artifact), patch.object(ac,'execute',return_value={'success':False}):
                state=ac.run({'max_attempts':3,'policy':'write_text'},'test',Path(tmp)/'run')
            self.assertEqual(state['status'],'failed')
            self.assertIn('repetido',state['error'])

if __name__=='__main__': unittest.main()
