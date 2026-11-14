# LLM Auto-Compiler (LLMAuc) — 0.1

**LLMAuc** stands for **LLM Auto Compiler**. A persistent base with an ephemeral Cognitive Core, inspired by HAME. Requires Python 3.10+ and the standard library; validated on Windows x64 with Python 3.14.

## Run

Copy `configs/config.example.json` to `config.json` in the repository root and set real llama-server and GGUF paths. The reference test used Gemma E4B Q4_K_M, Vulkan0, 20 layers, and a 4096-token context.

```powershell
python src/autocompiler.py --config config.json --objective 'Create hello.txt with Hello World and finish after checking the previous result.' --run-dir runs/my-task
python -m unittest discover -s tests -v
```

Use a new run directory for each invocation. Relative executable/model paths are resolved from the configuration directory. The Base does not modify models or the HAME installation.

## Implemented cycle

1. Persist the objective and checkpoint.
2. Start llama-server on loopback with a temporary port and `--tools all --parallel 1 --reasoning off --jinja`.
3. Wait for health, record the actual tools from `/tools`, and request a JSON contract containing analysis, review, action, script, and summary.
4. Capture the response and close the worker's entire Job Object before running the script.
5. Validate the contract and policy, persist the script and hash, run Python with a timeout, and record stdout, stderr, and the result.
6. Verify files and their contents under the default policy; provide the previous script, result, and history to a new process.
7. Accept completion only after successful execution. Stop repeated scripts and enforce the attempt limit.

On Windows, processes are created suspended and assigned to their own Job Object before they are resumed; closing the Base terminates its children. Processes are not stopped by name. Each inference starts a new worker and does not restore a session cache. The operating system's file cache may remain in RAM.

## Tools

`--tools all` is enabled in the server command and cannot be disabled in this version. The advertised list is saved for each attempt. Version 0.1 generates scripts: it does not send tool definitions to the model or forward calls to `/tools`. Unexpected calls are rejected. Enabling native tools is not the same as implementing a tool-call execution cycle. The server is bound to loopback and exists only during inference; it does not authenticate other local processes.

## Execution policies

`write_text` (default): accepts only a `Path` import and literal calls to `Path("relative-file").write_text("content", encoding="utf-8")`. It rejects directory traversal, links, overwrites, duplicate outputs, and dynamic expressions. The file must be in an existing directory. The Base checks the actual content after execution. This narrow policy validates the cycle; it is not a general sandbox or protection against concurrent filesystem changes.

`trusted_python`: runs arbitrary Python with the account's permissions. It is an explicit configuration choice with no sandbox; it was not enabled for real LLM tests. A zero exit code counts as process success but does not prove the objective was met. The LLM's textual review is not independent approval.

## Artifacts

`checkpoint.json`: state and history. `workspace/`: task outputs. `attempt-NNN/`: request, original response, artifact, script, hash, command and worker PID, timestamps, advertised tools, and separate logs.

Checkpoints are written through a temporary file and atomic replacement. `execution_pending` means an interruption may have happened before or during the script's effects: do not retry automatically. This version has no automatic resume, rollback, or exactly-once execution guarantee.

## Scope and limitations

This is a first working cycle for Windows, Python scripts, and the llama-server backend. There is no GUI, BAT/SH script support, separate inference for review, history compaction, context file selection, or protocol for scripts to request additional inference. History grows to the backend budget and may be rejected; it is not silently truncated. Logs are written to disk without rotation or a dedicated quota. Startup, request, and script timeouts are configurable. A contract failure stops execution and records an error checkpoint. Linux has not been run or tested. HAME is not a runtime dependency.

## Origin

Reference: https://github.com/themrsbusta/HAME (identified in the public README as 0.9 RC). Consulted copies and hashes are in `references/hame/manifest.json`. The local 0.92 RC installation is used only as a configuration reference. Reference sources retain the BSD-3-Clause license in `references/hame/LICENSE`. The Python adapter was written for this project; no installed HAME file was modified.

## Windows executable: LLMAuc.exe

The Windows package contains the console executable and its external configuration. Open `LLMAuc.exe` and enter an objective, or run:

```powershell
.\LLMAuc.exe --objective 'Create hello.txt with Hello World and finish after checking the previous result.'
```

Without `--run-dir`, runs are created in `runs` beside the executable. The default configuration is `config.json` beside it, regardless of the current directory. Python scripts run in a subprocess of LLMAuc itself with an embedded Python runtime, so Python does not need to be installed separately. Full llama.cpp and GGUF models remain external and must be available at the configured paths. `trusted_python` has access only to modules included in the package, not every Python package installed on the system.

Install build dependencies with `python -m pip install -r packaging/requirements-build.txt` and rebuild with `powershell -File scripts/windows/build-exe.ps1`. The executable and examples go in `build/windows-x64/`; the public ZIP goes in `build/packages/`. The script excludes personal configuration and history and preserves an existing `config.json`.

## 0.1 delivery layout

- `src/`: Python implementation. Default configuration and run paths remain relative to the checkout root; in the executable, they remain relative to the `LLMAuc.exe` directory.
- `tests/`: automated tests, run with the command above.
- `configs/`: public configuration example.
- `scripts/windows/`: launcher and packaging script. Start from PowerShell with `./scripts/windows/Start-AutoCompiler.ps1 -Objective 'Your objective'`.
- `packaging/`: PyInstaller specification and build dependencies.
- `docs/`: release notes and validation of the reorganization.
- `references/hame/`: preserved references and original manifest.
- `build/` and `runs/`: local outputs ignored by Git.

The reorganization retains version **0.1** and the existing cycle. Personal configuration, history, models, and intermediate files are not published.
