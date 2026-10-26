"""browser-use on a Baton browser: Baton starts the Chrome, browser-use's agent drives it over CDP.

    pip install -r requirements.txt
    BATON_API_KEY=bk_... OPENAI_API_KEY=sk-... python main.py
"""
import asyncio

from batonvm import AsyncBaton
from browser_use import Agent, Browser, ChatOpenAI


async def main():
    async with AsyncBaton() as baton:  # reads BATON_API_KEY
        # The session ends with the block: the browser is remote, and would otherwise run until its timeout.
        async with await baton.sessions.create() as session:
            agent = Agent(
                task="Find the top story on Hacker News and summarise its three most upvoted comments.",
                llm=ChatOpenAI(model="gpt-5-mini"),  # any model browser-use supports
                browser=Browser(cdp_url=session.connect_url),
            )
            result = await agent.run(max_steps=20)
            print(result.final_result())


asyncio.run(main())
