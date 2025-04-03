import { useState, useEffect } from 'react';
import axios from 'axios';
import Head from 'next/head';

export default function Home() {
  const [isHealthy, setIsHealthy] = useState(false);
  const [isLoading, setIsLoading] = useState(true);

  useEffect(() => {
    const checkHealth = async () => {
      try {
        const response = await axios.get(`${process.env.NEXT_PUBLIC_API_URL || 'http://localhost:3001'}/api/health`);
        setIsHealthy(response.data.status === 'OK');
      } catch (error) {
        console.error('Health check failed:', error);
        setIsHealthy(false);
      } finally {
        setIsLoading(false);
      }
    };

    checkHealth();
  }, []);

  return (
    <div className="container">
      <Head>
        <title>Falcon - Video on Demand Platform</title>
        <meta name="description" content="Open Source Video on Demand Platform" />
        <link rel="icon" href="/favicon.ico" />
      </Head>

      <main>
        <h1>Falcon Video Platform</h1>
        
        <div className="grid">
          <div className="card">
            <h2>API Status</h2>
            {isLoading ? (
              <p>Checking API status...</p>
            ) : isHealthy ? (
              <p className="status-healthy">API is running</p>
            ) : (
              <p className="status-error">API is not available</p>
            )}
          </div>

          <div className="card">
            <h2>Upload Video</h2>
            <p>Upload your videos for transcoding and streaming</p>
            <button
              onClick={() => window.location.href = '/upload'}
              className="button"
            >
              Go to Upload
            </button>
          </div>

          <div className="card">
            <h2>Videos</h2>
            <p>Manage and view your videos</p>
            <button
              onClick={() => window.location.href = '/videos'}
              className="button"
            >
              View Videos
            </button>
          </div>
        </div>
      </main>

      <footer>
        <p>Falcon - Open Source Video on Demand Platform</p>
      </footer>

      <style jsx>{`
        .container {
          min-height: 100vh;
          padding: 0 0.5rem;
          display: flex;
          flex-direction: column;
          justify-content: center;
          align-items: center;
        }

        main {
          padding: 5rem 0;
          flex: 1;
          display: flex;
          flex-direction: column;
          justify-content: center;
          align-items: center;
        }

        footer {
          width: 100%;
          height: 100px;
          border-top: 1px solid #eaeaea;
          display: flex;
          justify-content: center;
          align-items: center;
        }

        h1 {
          margin: 0;
          line-height: 1.15;
          font-size: 4rem;
          text-align: center;
        }

        .grid {
          display: flex;
          align-items: stretch;
          justify-content: center;
          flex-wrap: wrap;
          max-width: 900px;
          margin-top: 3rem;
        }

        .card {
          margin: 1rem;
          flex-basis: 45%;
          padding: 1.5rem;
          text-align: left;
          color: inherit;
          text-decoration: none;
          border: 1px solid #eaeaea;
          border-radius: 10px;
          transition: color 0.15s ease, border-color 0.15s ease;
        }

        .card h2 {
          margin: 0 0 1rem 0;
          font-size: 1.5rem;
        }

        .card p {
          margin: 0;
          font-size: 1.25rem;
          line-height: 1.5;
        }

        .button {
          margin-top: 1rem;
          padding: 0.5rem 1rem;
          background-color: #0070f3;
          color: white;
          border: none;
          border-radius: 5px;
          cursor: pointer;
          font-size: 1rem;
        }

        .status-healthy {
          color: green;
        }

        .status-error {
          color: red;
        }

        @media (max-width: 600px) {
          .grid {
            width: 100%;
            flex-direction: column;
          }
        }
      `}</style>
    </div>
  );
} 