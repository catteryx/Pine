# Changelog

## 0.3.0

- **One pi at a time instead of one per tab.** Only the session on screen and
  sessions with a task still running keep their pi; switching away stops the
  others, and showing a tab again resumes its session from its session file.
  Tabs restored at launch wait for their turn, so a window with many open tabs
  holds one pi rather than one per tab.
- A tab that has not been started yet still shows its own title in the sidebar,
  read from its session file.
- Stopping a session no longer holds up the window while pi shuts down.

## 0.2.0

- **Bundled pi runtime.** Pine ships a self-contained pi (compiled with Bun), so
  it runs without Node.js or npm installed. Settings → General chooses the
  runtime: Automatic, Bundled, or System, with an optional custom pi path.
- **Model services.** Settings → Model manages credentials and endpoints: store
  API keys for built-in providers in pi's auth.json, and add custom
  OpenAI-compatible endpoints to models.json. A custom endpoint can fetch its
  model list, and each model takes a context window and max output tokens.
- A settings window of its own, with General, Model and Appearance tabs, an
  appearance setting and a font size.
- Markdown links open in the browser; Markdown images load inline.
- The model and thinking level chosen in a session become the default for new
  sessions; a resumed session restores its own.

## 0.1.0

- First release: sessions in a resizable sidebar grouped by folder, a Markdown transcript with
  tool cards, diffs and image results, slash commands, MCP and Skills pages, file drag-and-drop
  with native icons, clipboard image paste, a context ring, settings, and signed auto-updates.
