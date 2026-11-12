const nextConfig = {
  allowedDevOrigins: ['192.168.89.197'],
  async rewrites() {
    return [
      {
        source: '/api/:path*',
        destination: 'http://localhost:8080/:path*',
      },
    ]
  },
}
module.exports = nextConfig