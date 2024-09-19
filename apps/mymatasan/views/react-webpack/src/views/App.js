import React, { useState, useEffect, useRef } from 'react'

const App = props => {
  return (
    <>
      <img
        id='videostream'
        src='https://localhost:3000/api/camera/stream/mjpeg/2'
        width='800px'
        height='600x'
        onError='https://bitsofco.de/img/Qo5mfYDE5v-350.png'
      />
      <img
        id='videostream'
        src='https://localhost:3000/api/camera/stream/mjpeg/3'
        width='800px'
        height='600x'
        onError='https://bitsofco.de/img/Qo5mfYDE5v-350.png'
      />
      <img
        id='videostream'
        src='https://localhost:3000/api/camera/stream/mjpeg/4'
        width='800px'
        height='600x'
        onError='https://bitsofco.de/img/Qo5mfYDE5v-350.png'
      />
      <img
        id='videostream'
        src='https://localhost:3000/api/camera/stream/mjpeg/1'
        width='800px'
        height='600x'
        onError='https://bitsofco.de/img/Qo5mfYDE5v-350.png'
      />
    </>
  )
}

export default App;
