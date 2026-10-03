# saved_recipes.md - example

This is the format histex appends to when you press `CTRL-T` inside the picker.

Your real `saved_recipes.md` is git-ignored (it holds your own commands); this
file only shows the shape of the data. Tags are optional and are written as an
HTML comment so they stay invisible when rendered.

### Date: 2026-01-01 12:00 - Local LLM setup   <!-- tags: llm, ollama -->

```bash
ollama list
ollama create llama3.1-locked -f Modelfile
```

### Date: 2026-01-02 09:30 - Unpack a tarball   <!-- tags: archive -->

```bash
tar -xzf archive.tar.gz
ls -la
```
