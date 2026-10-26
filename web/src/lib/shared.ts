/** The Baton repo, which the site describes. */
export const batonRepo = {
  user: "vedantxn",
  repo: "chromebox",
  url: "https://github.com/vedantxn/chromebox",
  license: "MIT",
};

/** The SDK on npm. */
export const sdk = { name: "batonvm", version: "0.3.1", url: "https://www.npmjs.com/package/batonvm" };
/** The Python SDK on PyPI (sdk-python/). */
export const pysdk = { name: "batonvm", version: "0.2.0", url: "https://pypi.org/project/batonvm/" };

/** A file in the Baton repo, on main. */
export function repoFile(path: string) {
  return `${batonRepo.url}/blob/main/${path}`;
}

/** A folder in the Baton repo, on main. */
export function repoTree(path: string) {
  return `${batonRepo.url}/tree/main/${path}`;
}
