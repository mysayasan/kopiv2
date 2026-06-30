import useInView from '../hooks/useInView.js';

// Wraps content in a scroll-reveal container. `as` picks the element, `stagger`
// adds incremental delays to direct children (via the --i custom property).
export default function Reveal({ as: Tag = 'div', stagger = false, className = '', children, ...rest }) {
  const [ref, inView] = useInView();
  const cls = ['reveal', stagger ? 'reveal--stagger' : '', inView ? 'is-in' : '', className]
    .filter(Boolean)
    .join(' ');
  return (
    <Tag ref={ref} className={cls} {...rest}>
      {children}
    </Tag>
  );
}
