// Stagehand on a Baton browser: Baton starts the Chrome, Stagehand drives it
// over CDP with act / extract / observe.
//
//   npm install
//   BATON_API_KEY=bk_... OPENAI_API_KEY=sk-... npm start
import { Baton } from 'batonvm';
import { Stagehand } from '@browserbasehq/stagehand';
import { z } from 'zod';

const baton = new Baton(); // reads BATON_API_KEY
const session = await baton.sessions.create({ viewport: { width: 1280, height: 800 } });
const stagehand = new Stagehand({
  env: 'LOCAL',
  localBrowserLaunchOptions: { cdpUrl: session.connectUrl },
  model: 'openai/gpt-5-mini', // reads OPENAI_API_KEY; any model Stagehand supports works
});
await stagehand.init();

try {
  const page = stagehand.context.pages()[0];
  await page.goto('https://news.ycombinator.com');

  await stagehand.act('open the comments of the top story');
  const story = await stagehand.extract(
    'the story title and the first three top-level comments',
    z.object({ title: z.string(), comments: z.array(z.string()).max(3) }),
  );
  console.log(story);

  // The browser can move to another machine mid-task; Stagehand's connection stays up.
  const move = await session.migrate();
  console.log(`moved ${move.from} → ${move.to}, paused ${move.pausedMs} ms`);
  console.log(await page.title());
} finally {
  await stagehand.close();
  await session.close(); // ends the Baton session: the browser is remote
}
