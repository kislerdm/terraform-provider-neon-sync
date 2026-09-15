export default {
  fetch: (_: Request) => {
    const env = process.env;
    const body = Object.keys(env).length === 0 ? "Hello world!" : JSON.stringify(env);

    return new Response(body, {
      headers: { "content-type": "text/plain; charset=utf-8" },
    });
  },
};
