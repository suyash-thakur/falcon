/** @type {import('next').NextConfig} */
const nextConfig = {
  reactStrictMode: true,
  swcMinify: true,
  // Configure environment variables if needed
  env: {
    API_URL: process.env.API_URL || 'http://localhost:3001',
  },
}

module.exports = nextConfig
