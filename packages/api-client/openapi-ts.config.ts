export default {
  input: './api/openapi.yaml',
  output: {
    path: './packages/api-client/src/generated',
  },
  plugins: [
    '@hey-api/client-fetch',
    {
      name: 'zod',
      definitions: true,
      requests: true,
      dates: {
        offset: true,
      },
    },
    {
      name: '@hey-api/sdk',
      validator: true,
    },
    {
      name: '@tanstack/react-query',
      queryOptions: true,
      mutationOptions: true,
      queryKeys: {
        tags: true,
      },
    },
  ],
}
