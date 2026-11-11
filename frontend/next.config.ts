import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  reactStrictMode: true,
  output: 'standalone',
  // Allow the host IP you use for development on the LAN.
  // You can add additional entries here (comma‑separated) or use '*'
  // while debugging locally. Remember to tighten this before production.
  allowedDevOrigins: ['192.168.89.197', '192.168.1.16'],
};

export default nextConfig;
