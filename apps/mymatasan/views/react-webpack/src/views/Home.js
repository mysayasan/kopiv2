import { Navigate } from 'react-router-dom';

// The landing route exists only to send "/" to the app.
//
// It carries the QUERY STRING with it. Redirecting to a bare "/app" dropped it, which
// silently broke every deep link into the app — found by the W3-3b screen pass, where the
// second-monitor window (`?wall=<id>`) arrived at "/app" with no wall to show and rendered
// the ordinary workspace instead. Anything the app ever reads out of the address bar has to
// survive this hop, so it is preserved here rather than at each reader.
const Home = () => <Navigate to={{ pathname: '/app', search: window.location.search }} replace />;

export default Home;
