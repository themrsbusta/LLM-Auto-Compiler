# LLM Auto-Compiler (LLMAuc) — 0.1

**LLMAuc** é a sigla de **LLM Auto Compiler**. Base permanente com Núcleo Cognitivo efêmero, inspirado no HAME. Python 3.10+ e biblioteca padrão; validado no Windows x64 com Python 3.14.

## Executar

Copie `config.example.json` para `config.json` e indique llama-server e GGUF reais. O teste de referência usou Gemma E4B Q4_K_M, Vulkan0, 20 camadas e contexto 4096.

```powershell
python autocompiler.py --config config.json --objective 'Crie hello.txt com Hello World e finalize após conferir o resultado anterior.' --run-dir runs/minha-tarefa
python -m unittest -v
```

Use uma pasta de execução nova a cada chamada. O caminho de executável/modelo relativo é resolvido pela pasta da configuração. A Base não altera modelos nem a instalação do HAME.

## Ciclo implementado

1. Persistir objetivo e checkpoint.
2. Iniciar llama-server em loopback, porta temporária, com `--tools all --parallel 1 --reasoning off --jinja`.
3. Aguardar saúde, registrar ferramentas reais de `/tools` e solicitar contrato JSON: análise, revisão, ação, script e resumo.
4. Capturar resposta e encerrar todo o Job Object do worker antes de executar o script.
5. Validar contrato e política, persistir script e hash, executar Python com timeout, registrar stdout, stderr e resultado.
6. Verificar arquivos e conteúdo na política padrão; fornecer script anterior, resultado e histórico a um processo novo.
7. Aceitar finalização somente depois de execução bem-sucedida. Interromper scripts repetidos e aplicar máximo de tentativas.

No Windows, processos nascem suspensos e são atribuídos a um Job Object próprio antes de serem liberados; fechamento da Base encerra os filhos. Não são encerrados processos por nome. Cada inferência carrega um novo worker e nenhum cache de sessão é restaurado. O cache de arquivos do sistema operacional pode continuar em RAM.

## Ferramentas

`--tools all` está ativado no comando do servidor, sem opção de desativação nesta versão. A lista anunciada é salva em cada tentativa. O fluxo 0.1 usa geração de scripts: não passa definições de ferramentas ao modelo nem encaminha chamadas para `/tools`. Chamadas inesperadas são rejeitadas. Habilitar as ferramentas nativas não equivale a implementar o ciclo de execução de tool calls. O servidor fica em loopback e existe somente durante a inferência; não oferece autenticação a outros processos locais.

## Políticas de execução

`write_text` (padrão): aceita somente importação de Path e chamadas literais `Path("arquivo-relativo").write_text("conteúdo", encoding="utf-8")`. Rejeita escapes de diretório, links, sobrescrita, saídas duplicadas e expressões dinâmicas. O arquivo deve estar em diretório já existente. A Base verifica o conteúdo real após executar. É uma política estreita para validar o ciclo, não um sandbox geral nem proteção contra alterações concorrentes no filesystem.

`trusted_python`: executa Python arbitrário com as permissões da conta. É uma seleção explícita na configuração, sem sandbox; não foi ativada para os testes reais com a LLM. O retorno zero conta como sucesso de processo, sem provar que o objetivo foi cumprido. A revisão textual da LLM não constitui aprovação independente.

## Artefatos

`checkpoint.json`: estado e histórico. `workspace/`: saídas da tarefa. `attempt-NNN/`: requisição, resposta original, artefato, script, hash, comando e PID do worker, timestamps, ferramentas anunciadas e logs separados.

Checkpoints são gravados via arquivo temporário e substituição atômica. `execution_pending` indica que uma interrupção pode ter acontecido antes ou durante os efeitos do script: não repetir automaticamente. Não há retomada automática, rollback ou garantia de execução exatamente uma vez nesta versão.

## Escopo e limites

Primeiro ciclo funcional para Windows, scripts Python e backend llama-server. Ainda não há interface gráfica, scripts BAT/SH, revisão em inferência separada, compactação do histórico, seleção de arquivos para contexto ou protocolo de novas inferências solicitadas por scripts. O histórico cresce até o orçamento do backend e pode causar rejeição; não há truncamento silencioso. Logs vão para disco, sem rotação ou quota própria. Timeout de inicialização, requisição e script é configurável. Falha de contrato encerra a execução com checkpoint de erro. Linux não foi executado/testado. Sem dependência de HAME em runtime.

## Origem

Referência: https://github.com/themrsbusta/HAME (identificado no README público como 0.9 RC). Cópias consultadas e hashes em `references/hame/manifest.json`. A instalação local 0.92 RC é usada somente como referência de configuração. Fontes de referência preservam a licença BSD-3-Clause em `references/hame/LICENSE`. O adaptador Python foi escrito para este projeto; nenhum arquivo instalado do HAME foi modificado.

## Executável Windows LLMAuc.exe

O pacote Windows contém o executável de console e sua configuração externa. Abra `LLMAuc.exe` e digite o objetivo, ou use:

```powershell
.\LLMAuc.exe --objective 'Crie hello.txt com Hello World e finalize após conferir o resultado anterior.'
```

Sem `--run-dir`, as execuções são criadas em `runs` ao lado do executável. A configuração padrão é `config.json` ao lado dele, independentemente do diretório atual. Os scripts Python são executados em um subprocesso do próprio LLMAuc, com runtime Python embutido; não é necessário instalar Python separadamente. llama.cpp completo e modelos GGUF continuam externos e precisam existir nos caminhos da configuração. O modo `trusted_python` só dispõe dos módulos incluídos no pacote, não de qualquer pacote Python instalado no sistema.

Reconstruir com Python e PyInstaller instalados: `powershell -File build-exe.ps1`.

## Organização da entrega 0.1

No repositório público, os fontes ficam na raiz. O script de empacotamento produz o diretório `build/` e o ZIP ao lado do checkout. Configurações pessoais, históricos e intermediários não são publicados.
