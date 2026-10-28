# LLMAuc 0.1

LLM Auto-Compiler: primeiro protótipo funcional de Núcleo Cognitivo Efêmero.

Validado no Windows x64 com llama.cpp b11946 Vulkan e Gemma E4B Q4_K_M: duas inferências em processos distintos, geração e execução de script Python e conferência exata do arquivo Hello World. Seis testes automatizados passaram.

`--tools all` habilitado no servidor. O encaminhamento de tool calls nativas ainda não está implementado. Execução padrão restrita à criação de arquivos de texto. Sem retomada automática ou rollback.

O ZIP inclui runtime Python; llama.cpp e modelos não estão incluídos. Copie config.example.json para config.json e configure os caminhos antes de executar.
