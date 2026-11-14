# Reorganization validation — 2026-10-28

Version remains 0.1. Code, tests, and build files were moved into their own directories. The default source-mode path was adjusted to keep using `config.json` and `runs/` in the root; the executable continues to use its own directory.

- Six automated tests passed with Python 3.14.8 on Windows x64.
- Executable rebuilt with PyInstaller 6.22.3 from `packaging/LLMAuc.spec`; `LLMAuc.exe --help` passed.
- Public ZIP contains only the executable, public example, documentation, version, and license. Personal configuration and runs are excluded.
- HAME reference sources and the manifest preserve the original repository blobs.

This reorganization does not repeat the real inference test described in the 0.1 release notes and does not validate Linux. Tooling, policy, and resume limitations remain as documented in the README.
