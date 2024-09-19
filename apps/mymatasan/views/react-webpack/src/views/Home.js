import React, { useState, useEffect, useRef } from 'react'
import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom';

const Home = props => {
    return <Navigate to="/app" replace />
}

export default Home;
