/// <reference types="@welldone-software/why-did-you-render" />

/**
 * To track a specific component:
 * 
 * MyComponent.whyDidYouRender = true;
 * 
 * Or for functional components:
 * 
 * const MyComponent = () => ...
 * MyComponent.whyDidYouRender = true;
 */

// Temporarily disabled due to React 18/19 compatibility issue
// Error: "Cannot create property 'current' on boolean 'false'"
// To re-enable when library is updated:
// 1. Uncomment the import: import React from 'react';
// 2. Uncomment the code below:
// if (import.meta.env.DEV) {
//   const { default: whyDidYouRender } = await import('@welldone-software/why-did-you-render');
//   whyDidYouRender(React, {
//     trackAllPureComponents: false,
//     logOnDifferentValues: true,
//   });
// }
