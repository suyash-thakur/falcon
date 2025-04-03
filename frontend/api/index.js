const express = require('express');
const bodyParser = require('body-parser');
const cors = require('cors');
const multer = require('multer');
const { v4: uuidv4 } = require('uuid');
const path = require('path');
const dotenv = require('dotenv');

// Load environment variables
dotenv.config();

const app = express();
const PORT = process.env.API_PORT || 3001;

// Middleware
app.use(cors());
app.use(bodyParser.json());
app.use(bodyParser.urlencoded({ extended: true }));

// Set up multer for file uploads
const storage = multer.diskStorage({
  destination: (req, file, cb) => {
    cb(null, path.join(__dirname, '../uploads/'));
  },
  filename: (req, file, cb) => {
    const uniqueFileName = `${uuidv4()}${path.extname(file.originalname)}`;
    cb(null, uniqueFileName);
  }
});

const upload = multer({
  storage,
  limits: { fileSize: 500 * 1024 * 1024 }, // 500MB limit
  fileFilter: (req, file, cb) => {
    // Accept video files only
    const filetypes = /mp4|mov|avi|wmv|mkv/;
    const mimetype = filetypes.test(file.mimetype);
    const extname = filetypes.test(path.extname(file.originalname).toLowerCase());
    
    if (mimetype && extname) {
      return cb(null, true);
    }
    
    cb(new Error('Error: Only video files are allowed!'));
  }
});

// Routes
app.get('/api/health', (req, res) => {
  res.status(200).json({ status: 'OK', message: 'API is running' });
});

// Video upload endpoint
app.post('/api/videos/upload', upload.single('video'), (req, res) => {
  try {
    if (!req.file) {
      return res.status(400).json({ error: 'No video file provided' });
    }

    // Here we would normally call the Go backend to process the video
    // For now, just return success response with the file details
    res.status(200).json({
      message: 'Video uploaded successfully',
      fileName: req.file.filename,
      originalName: req.file.originalname,
      size: req.file.size,
      path: req.file.path,
      // This would typically include a job ID or video ID from the backend
      videoId: uuidv4()
    });
  } catch (error) {
    console.error('Error uploading video:', error);
    res.status(500).json({ error: 'Error uploading video' });
  }
});

// Get video status endpoint
app.get('/api/videos/:id/status', (req, res) => {
  // This would normally query the Go backend for the video status
  res.status(200).json({
    videoId: req.params.id,
    status: 'processing', // or 'completed', 'failed', etc.
    progress: Math.floor(Math.random() * 100) // Mock progress
  });
});

// Error handling middleware
app.use((err, req, res, next) => {
  console.error(err.stack);
  res.status(500).json({ error: err.message || 'Something went wrong!' });
});

// Start the server
app.listen(PORT, () => {
  console.log(`API server running on port ${PORT}`);
});

module.exports = app;
