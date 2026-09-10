import assert from 'node:assert/strict'
import { test } from 'node:test'
import { createJsonResponseHandler, createJsonErrorResponseHandler, createStatusCodeErrorResponseHandler, jsonSchema } from '@ai-sdk/provider-utils'

test('provider JSON and error readers accept ordinary responses and cancel oversized bodies', async () => {
  const schema = jsonSchema({ type: 'object' })
  const handlers = [
    [createJsonResponseHandler(schema), value => assert.deepEqual(value, { message: 'ok' })],
    [createJsonErrorResponseHandler({ errorSchema: schema, errorToMessage: value => value.message }), value => assert.equal(value.message, 'ok')],
    [createStatusCodeErrorResponseHandler(), value => assert.equal(value.responseBody, '{"message":"ok"}')],
  ]
  const input = { url: 'https://provider.invalid/response', requestBodyValues: {} }
  // Reuse immutable bytes to cross the real 2 GiB cumulative limit with 1 MiB
  // of fixture memory. An error at the next pull prevents a full-body join.
  const chunk = new Uint8Array(1024 * 1024)
  for (const [handler, check] of handlers) {
    check((await handler({ ...input, response: Response.json({ message: 'ok' }) })).value)
    for (const advertised of [false, true]) {
      let pulls = 0, cancelled = false
      const body = new ReadableStream({
        pull(controller) {
          if (pulls++ < 2049) controller.enqueue(chunk)
          else controller.error(new Error('reader exceeded the fixture safety boundary'))
        },
        cancel() { cancelled = true },
      }, { highWaterMark: 0 })
      const headers = advertised ? { 'content-length': '2147483649' } : {}
      await assert.rejects(handler({ ...input, response: new Response(body, { headers }) }), /exceeded maximum size/)
      assert.equal(pulls, advertised ? 0 : 2049, 'reject at the header or first over-limit chunk')
      assert.equal(cancelled, true, 'refusal cancels the upstream body')
    }
  }
})
