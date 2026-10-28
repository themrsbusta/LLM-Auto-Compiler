# Validação da organização — 2026-10-28

A versão permanece 0.1. O código, os testes e os arquivos de build foram movidos para diretórios próprios. O caminho padrão no modo fonte foi ajustado para continuar usando `config.json` e `runs/` na raiz; o executável continua usando sua própria pasta.

- Seis testes automatizados aprovados com Python 3.14.8 no Windows x64.
- Executável reconstruído com PyInstaller 6.22.3 a partir de `packaging/LLMAuc.spec`; `LLMAuc.exe --help` aprovado.
- ZIP público gerado somente com executável, exemplo público, documentação, versão e licença. Configurações pessoais e execuções ficam fora dele.
- As fontes de referência HAME e o manifesto preservam os blobs do repositório original.

Esta reorganização não repete o teste de inferência real descrito nas notas 0.1 e não valida Linux. As limitações de ferramentas, políticas e retomada permanecem as documentadas no README.
