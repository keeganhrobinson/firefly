import { app, mockEventStreamWebSocket } from './common';
import nock from 'nock';
import request from 'supertest';
import assert from 'assert';
// // import { IEventMemberRegistered } from '../lib/interfaces';

describe('Asset definitions', async () => {

  it('Checks that an empty array is initially returned when querying asset definitions', async () => {
    const result = await request(app)
      .get('/api/v1/assets/definitions')
      .expect(200);
    assert.deepStrictEqual(result.body, []);
  });

  it('Attempting to add an asset definition without a name should raise an error', async () => {
    const result = await request(app)
      .post('/api/v1/assets/definitions')
      .send({
        author: '0x0000000000000000000000000000000000000001',
        isContentPrivate: false
      })
      .expect(400);
    assert.deepStrictEqual(result.body, { error: 'Missing or invalid asset definition name' });
  });

  it('Attempting to add an asset definition without an author should raise an error', async () => {
    const result = await request(app)
      .post('/api/v1/assets/definitions')
      .send({
        name: 'Undescribed - unstructured',
        isContentPrivate: false
      })
      .expect(400);
    assert.deepStrictEqual(result.body, { error: 'Missing asset definition author' });
  });

  it('Attempting to add an asset definition without indicating if the content should be private or not should raise an error', async () => {
    const result = await request(app)
      .post('/api/v1/assets/definitions')
      .send({
        name: 'Undescribed - unstructured',
        author: '0x0000000000000000000000000000000000000001'
      })
      .expect(400);
    assert.deepStrictEqual(result.body, { error: 'Missing asset definition content privacy' });
  });


  it('Checks that an asset definition can be added - undescribed, unstructured public content', async () => {

    nock('https://apigateway.kaleido.io')
      .post('/createUnstructuredAssetDefinition?kld-from=0x0000000000000000000000000000000000000001&kld-sync=true')
      .reply(200);

    const result = await request(app)
      .post('/api/v1/assets/definitions')
      .send({
        name: 'Undescribed - unstructured',
        author: '0x0000000000000000000000000000000000000001',
        isContentPrivate: false
      })
      .expect(200);
    assert.deepStrictEqual(result.body, { status: 'submitted' });
  });

});
