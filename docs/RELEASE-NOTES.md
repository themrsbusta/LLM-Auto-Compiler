# LLMAuc 0.1

LLM Auto-Compiler: first working prototype of an Ephemeral Cognitive Core.

Validated on Windows x64 with llama.cpp b11946 Vulkan and Gemma E4B Q4_K_M: two inferences in separate processes, Python script generation and execution, and exact verification of the Hello World file. Six automated tests passed.

`--tools all` is enabled on the server. Forwarding native tool calls is not implemented yet. Default execution is restricted to creating text files. There is no automatic resume or rollback.

The ZIP includes the Python runtime; llama.cpp and models are not included. Copy `config.example.json` to `config.json` and configure paths before running.
