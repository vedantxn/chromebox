# browser-use on Baton

[browser-use](https://github.com/browser-use/browser-use) takes a CDP URL, so its agent runs on a Baton browser with one argument:

```python
async with await baton.sessions.create() as session:
    agent = Agent(task="...", llm=ChatOpenAI(model="gpt-5-mini"), browser=Browser(cdp_url=session.connect_url))
    await agent.run()
```

```bash
pip install -r requirements.txt
BATON_API_KEY=bk_... OPENAI_API_KEY=sk-... python main.py
```

- Any model browser-use supports works (`ChatAnthropic`, `ChatGoogle`, ...).
- Close the Baton session when the agent is done (the `async with` block does). `Browser.stop()` only disconnects; the remote browser would run until its timeout.
- The session can `migrate()` while the agent works: its connection stays up.

Checked against Baton with browser-use 0.13.11 on Python 3.14: connecting, reading the page's elements, a migration between steps, and the agent loop reaching the model.
